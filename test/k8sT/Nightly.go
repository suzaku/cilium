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
)

var (
	endpointCount    = 45
	endpointsTimeout = 1 * time.Minute * time.Duration(endpointCount)
)

var deleteAll = func(kubectl *helpers.Kubectl) {
	ctx, cancel := context.WithTimeout(context.Background(), endpointsTimeout)
	defer cancel()
	kubectl.ExecInBackground(ctx, fmt.Sprintf(
		"%s delete --all pods,svc,cnp -n %s --grace-period=0 --force",
		helpers.KubectlCmd, helpers.DefaultNamespace))

	select {
	case <-ctx.Done():
		logger.Errorf("DeleteAll: delete all pods,services failed after %s", helpers.HelperTimeout)
	}
}

var _ = Describe("NightlyEpsMeasurement", func() {
	var kubectl *helpers.Kubectl
	var ciliumFilename string

	BeforeAll(func() {
		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)

		ciliumFilename = helpers.TimestampFilename("cilium.yaml")
		DeployCiliumAndDNS(kubectl, ciliumFilename)
	})

	AfterAll(func() {
		kubectl.DeleteCiliumDS()
		UninstallCiliumFromManifest(kubectl, ciliumFilename)
		kubectl.CloseSSHClient()
	})

	AfterFailed(func() {
		kubectl.CiliumReport("cilium service list", "cilium endpoint list")
	})

	JustAfterEach(func() {
		kubectl.ValidateNoErrorsInLogs(CurrentGinkgoTestDescription().Duration)
	})

	AfterEach(func() {
		ExpectAllPodsTerminated(kubectl)
	})

	Context("Endpoint", endpointTest)

	Context("Nightly Policies", policyTests)

	Context("Test long live connections", longLiveConnectionTests)
})

var _ = Describe("NightlyExamples", func() {
	var kubectl *helpers.Kubectl
	var l3Policy string
	var ciliumFilename string

	BeforeAll(func() {
		kubectl = helpers.CreateKubectl(helpers.K8s1VMName(), logger)

		demoPath = helpers.ManifestGet(kubectl.BasePath(), "demo.yaml")
		l3Policy = helpers.ManifestGet(kubectl.BasePath(), "l3-l4-policy.yaml")
		l7Policy = helpers.ManifestGet(kubectl.BasePath(), "l7-policy.yaml")

		ciliumFilename = helpers.TimestampFilename("cilium.yaml")
		DeployCiliumAndDNS(kubectl, ciliumFilename)

	})

	AfterFailed(func() {
		kubectl.CiliumReport("cilium service list", "cilium endpoint list")
	})

	JustAfterEach(func() {
		kubectl.ValidateNoErrorsInLogs(CurrentGinkgoTestDescription().Duration)
	})

	AfterEach(func() {
		kubectl.Delete(demoPath)
		kubectl.Delete(l3Policy)
		kubectl.Delete(l7Policy)

		ExpectAllPodsTerminated(kubectl)
	})

	AfterAll(func() {
		UninstallCiliumFromManifest(kubectl, ciliumFilename)
		kubectl.CloseSSHClient()
	})

	Context("Upgrade test", nightlyUpgradeTest)

	Context("Getting started guides", grpcTest)
})
