// Copyright 2017-2020 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package k8sTest

import (
	"context"
	"fmt"
	"time"

	. "github.com/cilium/cilium/test/ginkgo-ext"
	"github.com/cilium/cilium/test/helpers"

	. "github.com/onsi/gomega"
)

var longLiveConnectionTests = func() {
	var (
		kubectl *helpers.Kubectl

		netcatDsManifest = "netcat-ds.yaml"
	)

	getServer := func(port string) string {
		return fmt.Sprintf("nc -p %s -lk -v", port)
	}

	getClient := func(ip, port, filePipe string) string {
		return fmt.Sprintf(
			`bash -c "rm %[1]s; touch %[1]s; tail -f %[1]s 2>&1 | nc -v %[2]s %[3]s"`,
			filePipe, ip, port)
	}

	HTTPRequest := func(uid, host string) string {
		request := `GET /public HTTP/1.1\r\n` +
			`host: %s:8888\r\n` +
			`user-agent: curl/7.54.0\r\n` +
			`accept: */*\r\n` +
			`UID: %s\r\n` +
			`content-length: 0\r\n`
		return fmt.Sprintf(request, host, uid)
	}
	// testConnectivity check that nc is running across the k8s nodes
	testConnectivity := func() {
		killNetcat := "killall nc"
		pipePath := "/tmp/nc_pipe.txt"
		listeningString := "listening on [::]:8888"

		err := kubectl.WaitforPods(helpers.DefaultNamespace, "-l zgroup=netcatds", helpers.HelperTimeout)
		Expect(err).To(BeNil(), "Pods are not ready after timeout")

		netcatPods, err := kubectl.GetPodNames(helpers.DefaultNamespace, "zgroup=netcatds")
		Expect(err).To(BeNil(), "Cannot get pods names for netcatds")
		Expect(len(netcatPods)).To(BeNumerically(">=", 2), "Pods are not ready")

		server := netcatPods[0]
		client := netcatPods[1]
		ips, err := kubectl.GetPodsIPs(helpers.DefaultNamespace, "zgroup=netcatds")
		Expect(err).To(BeNil(), "Cannot get netcat ips")

		ncServer := getServer("8888")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		testNcConnectivity := func(sleep time.Duration) {
			uid := helpers.MakeUID()
			pipePath = "/tmp/" + uid
			ncClient := getClient(ips[server], "8888", pipePath)

			kubectl.ExecPodCmd(helpers.DefaultNamespace, server, killNetcat)
			serverctx := kubectl.ExecPodCmdBackground(ctx, helpers.DefaultNamespace, server, ncServer)
			err = serverctx.WaitUntilMatch(listeningString)
			Expect(err).To(BeNil(), "netcat server did not start correctly")

			kubectl.ExecPodCmd(helpers.DefaultNamespace, client, killNetcat)
			kubectl.ExecPodCmdBackground(ctx, helpers.DefaultNamespace, client, ncClient)

			helpers.Sleep(sleep)

			res := kubectl.ExecPodCmd(helpers.DefaultNamespace, client,
				fmt.Sprintf(`bash -c "echo -e '%[1]s' >> %s"`, HTTPRequest(uid, ips[client]), pipePath))
			res.ExpectSuccess("Failed to populate netcat client pipe")

			Expect(serverctx.WaitUntilMatch(uid)).To(BeNil(),
				"%q is not in the server output after timeout", uid)
			serverctx.ExpectContains(uid, "Cannot get server UUID")
		}
		By("Testing that simple nc works")
		testNcConnectivity(1)

		By("Sleeping for a minute to check tcp-keepalive")
		testNcConnectivity(60)

		By("Sleeping for six  minutes to check tcp-keepalive")
		testNcConnectivity(360)
	}

	BeforeAll(func() {
		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)
	})

	AfterAll(func() {
		kubectl.CloseSSHClient()
	})

	It("Test TCP Keepalive with L7 Policy", func() {
		kubectl.ValidateNoErrorsInLogs(CurrentGinkgoTestDescription().Duration)
		manifest := helpers.ManifestGet(kubectl.BasePath(), netcatDsManifest)
		kubectl.ApplyDefault(manifest).ExpectSuccess("Cannot apply netcat ds")
		defer kubectl.Delete(manifest)
		testConnectivity()
	})

	It("Test TCP Keepalive without L7 Policy", func() {
		manifest := helpers.ManifestGet(kubectl.BasePath(), netcatDsManifest)
		kubectl.ApplyDefault(manifest).ExpectSuccess("Cannot apply netcat ds")
		defer kubectl.Delete(manifest)
		kubectl.Exec(fmt.Sprintf(
			"%s delete --all cnp -n %s", helpers.KubectlCmd, helpers.DefaultNamespace))
		testConnectivity()
	})
}

var grpcTest = func() {
	var (
		kubectl        *helpers.Kubectl
		ciliumFilename string

		GRPCManifest = "../examples/kubernetes-grpc/cc-door-app.yaml"
		GRPCPolicy   = "../examples/kubernetes-grpc/cc-door-ingress-security.yaml"

		AppManifest    = ""
		PolicyManifest = ""
	)

	BeforeAll(func() {
		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)

		AppManifest = kubectl.GetFilePath(GRPCManifest)
		PolicyManifest = kubectl.GetFilePath(GRPCPolicy)

		ciliumFilename = helpers.TimestampFilename("cilium.yaml")
		DeployCiliumAndDNS(kubectl, ciliumFilename)
	})

	AfterAll(func() {
		kubectl.Delete(AppManifest)
		kubectl.Delete(PolicyManifest)
		ExpectAllPodsTerminated(kubectl)

		kubectl.CloseSSHClient()
	})

	It("GRPC example", func() {
		clientPod := "terminal-87"

		By("Testing the example config")
		kubectl.ApplyDefault(AppManifest).ExpectSuccess("cannot install the GRPC application")

		err := kubectl.WaitforPods(helpers.DefaultNamespace, "-l zgroup=grpcExample", helpers.HelperTimeout)
		Expect(err).Should(BeNil(), "Pods are not ready after timeout")

		res := kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py GetName 1")
		res.ExpectSuccess("Client cannot get Name")

		res = kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py GetLocation 1")
		res.ExpectSuccess("Client cannot get Location")

		res = kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py SetAccessCode 1 999")
		res.ExpectSuccess("Client cannot set Accesscode")

		By("Testing with L7 policy")
		_, err = kubectl.CiliumPolicyAction(
			helpers.DefaultNamespace, PolicyManifest,
			helpers.KubectlApply, helpers.HelperTimeout)
		Expect(err).To(BeNil(), "Cannot import GPRC policy")

		res = kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py GetName 1")
		res.ExpectSuccess("Client cannot get Name")

		res = kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py GetLocation 1")
		res.ExpectSuccess("Client cannot get Location")

		res = kubectl.ExecPodCmd(
			helpers.DefaultNamespace, clientPod,
			"python3 /cloudcity/cc_door_client.py SetAccessCode 1 999")
		res.ExpectFail("Client can set Accesscode and it shoud not")
	})
}
