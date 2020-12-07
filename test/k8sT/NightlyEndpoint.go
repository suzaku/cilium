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
	"path"
	"strings"
	"time"

	"github.com/cilium/cilium/api/v1/models"
	. "github.com/cilium/cilium/test/ginkgo-ext"
	"github.com/cilium/cilium/test/helpers"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var endpointTest = func() {
	var kubectl *helpers.Kubectl
	manifestPath := "tmp.yaml"
	vagrantManifestPath := ""

	deployEndpoints := func() {
		_, _, err := helpers.GenerateManifestForEndpoints(endpointCount, manifestPath)
		ExpectWithOffset(1, err).Should(BeNil(), "Manifest cannot be created correctly")

		// This is equivalent to res := kubectl.Apply(vagrantManifestPath) but we
		// need a longer timeout than helpers.ShortCommandTimeout
		ctx, cancel := context.WithTimeout(context.Background(), endpointsTimeout)
		defer cancel()
		res := kubectl.ExecContext(ctx, fmt.Sprintf("%s apply -f  %s", helpers.KubectlCmd, vagrantManifestPath))
		res.ExpectSuccess("cannot apply eps manifest :%s", res.GetDebugMessage())
	}

	getServices := func() map[string]string {
		// getServices returns a map of services, where service name is the key
		// and the ClusterIP is the value.
		services, err := kubectl.Get(helpers.DefaultNamespace, "services -l zgroup=testapp").Filter(
			`{range .items[*]}{.metadata.name}{"="}{.spec.clusterIP}{"\n"}{end}`)
		ExpectWithOffset(1, err).To(BeNil(), "cannot retrieve testapp services")
		result := make(map[string]string)
		for _, line := range strings.Split(services.String(), "\n") {
			vals := strings.Split(line, "=")
			if len(vals) == 2 {
				result[vals[0]] = vals[1]
			}
		}
		return result
	}

	BeforeAll(func() {
		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)
		vagrantManifestPath = path.Join(kubectl.BasePath(), manifestPath)
	})

	AfterAll(func() {
		kubectl.Delete(vagrantManifestPath)
		deleteAll(kubectl)
		ExpectAllPodsTerminated(kubectl)
		kubectl.CloseSSHClient()
	})

	Measure("The endpoint creation", func(b ginkgo.Benchmarker) {
		desiredState := string(models.EndpointStateReady)

		deployEndpoints()
		waitForPodsTime := b.Time("Wait for pods", func() {
			err := kubectl.WaitforPods(helpers.DefaultNamespace, "-l zgroup=testapp", endpointsTimeout)
			Expect(err).Should(BeNil(),
				"Cannot retrieve %d pods in %d seconds", endpointCount, endpointsTimeout.Seconds())
		})

		log.WithFields(logrus.Fields{"pod creation time": waitForPodsTime}).Info("")

		ciliumPods, err := kubectl.GetCiliumPods()
		Expect(err).To(BeNil(), "Cannot retrieve cilium pods")

		runtime := b.Time("Endpoint creation", func() {
			Eventually(func() bool {
				count := 0
				for _, pod := range ciliumPods {
					status := kubectl.CiliumEndpointsStatus(pod)
					result := map[string]int{}
					for _, state := range status {
						result[state]++
					}
					count += result[desiredState]
					logger.WithFields(logrus.Fields{
						"status": result,
						"pod":    pod,
					}).Info("Cilium endpoint status")
				}
				return count >= endpointCount
			}, endpointsTimeout, 3*time.Second).Should(BeTrue())
		})
		log.WithFields(logrus.Fields{"endpoint creation time": runtime}).Info("")

		services := getServices()
		Expect(len(services)).To(BeNumerically(">", 0), "Was not able to get services")

		pods, err := kubectl.GetPodNames(helpers.DefaultNamespace, "zgroup=testapp")
		Expect(err).To(BeNil(), "cannot retrieve pods names")

		err = kubectl.WaitforPods(helpers.DefaultNamespace, "", helpers.HelperTimeout)
		Expect(err).Should(BeNil(), "Pods are not ready after timeout")

		By("Testing if http requests to multiple endpoints do not timeout")
		for i := 0; i < 5; i++ {
			for _, pod := range pods {
				for service, ip := range services {
					b.Time("Curl to service", func() {

						res := kubectl.ExecPodCmd(
							helpers.DefaultNamespace, pod,
							helpers.CurlFail(fmt.Sprintf("http://%s:80/", ip)))
						res.ExpectSuccess(
							"Cannot curl from %s to service %s on  ip %s", pod, service, ip)
					})
				}

			}
		}
	}, 1)
}
