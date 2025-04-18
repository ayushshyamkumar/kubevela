/*
Copyright 2021 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	cuexv1alpha1 "github.com/kubevela/pkg/apis/cue/v1alpha1"
	"github.com/kubevela/pkg/util/singleton"
	terraformv1beta2 "github.com/oam-dev/terraform-controller/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	"github.com/oam-dev/kubevela/pkg/appfile"
	"github.com/oam-dev/kubevela/pkg/multicluster"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
var cfg *rest.Config
var recorder = NewFakeRecorder(10000)
var k8sClient client.Client
var testEnv *envtest.Environment
var testScheme = runtime.NewScheme()
var reconciler *Reconciler
var appParser *appfile.Parser
var controllerDone context.CancelFunc
var mgr ctrl.Manager
var appRevisionLimit = 5

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))
	rand.Seed(time.Now().UnixNano())
	By("bootstrapping test environment")
	var yamlPath string
	if _, set := os.LookupEnv("COMPATIBILITY_TEST"); set {
		yamlPath = "../../../../../test/compatibility-test/testdata"
	} else {
		yamlPath = filepath.Join("../../../../..", "charts", "vela-core", "crds")
	}
	logf.Log.Info("start application suit test", "yaml_path", yamlPath)
	testEnv = &envtest.Environment{
		ControlPlaneStartTimeout: time.Minute,
		ControlPlaneStopTimeout:  time.Minute,
		UseExistingCluster:       ptr.To(false),
		CRDDirectoryPaths:        []string{yamlPath, "./testdata/crds/terraform.core.oam.dev_configurations.yaml"},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = v1beta1.SchemeBuilder.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	err = scheme.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	err = terraformv1beta2.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = crdv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = cuexv1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme
	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())
	singleton.KubeClient.Set(k8sClient)
	fakeDynamicClient := fake.NewSimpleDynamicClient(testScheme)
	singleton.DynamicClient.Set(fakeDynamicClient)
	appParser = appfile.NewApplicationParser(k8sClient)

	reconciler = &Reconciler{
		Client:   k8sClient,
		Scheme:   testScheme,
		Recorder: event.NewAPIRecorder(recorder),
	}

	reconciler.appRevisionLimit = appRevisionLimit
	// setup the controller manager since we need the component handler to run in the background
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		LeaderElection:          false,
		LeaderElectionNamespace: "default",
		LeaderElectionID:        "test",
	})
	Expect(err).NotTo(HaveOccurred())
	definitionNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "vela-system"}}
	Expect(k8sClient.Create(context.Background(), definitionNs.DeepCopy())).Should(BeNil())

	var ctx context.Context
	ctx, controllerDone = context.WithCancel(context.Background())
	// start the controller in the background so that new componentRevisions are created
	go func() {
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
	multicluster.InitClusterInfo(cfg)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if controllerDone != nil {
		controllerDone()
	}
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

type FakeRecorder struct {
	Events  chan string
	Message map[string][]*Events
}

type Events struct {
	Name      string
	Namespace string
	EventType string
	Reason    string
	Message   string
}

func (f *FakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	if f.Events != nil {
		objectMeta, err := meta.Accessor(object)
		if err != nil {
			return
		}

		event := &Events{
			Name:      objectMeta.GetName(),
			Namespace: objectMeta.GetNamespace(),
			EventType: eventtype,
			Reason:    reason,
			Message:   message,
		}

		records, ok := f.Message[objectMeta.GetName()]
		if !ok {
			f.Message[objectMeta.GetName()] = []*Events{event}
			return
		}

		records = append(records, event)
		f.Message[objectMeta.GetName()] = records

	}
}

func (f *FakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Event(object, eventtype, reason, messageFmt)
}

func (f *FakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Eventf(object, eventtype, reason, messageFmt, args...)
}

func (f *FakeRecorder) GetEventsWithName(name string) ([]*Events, error) {
	records, ok := f.Message[name]
	if !ok {
		return nil, errors.New("not found events")
	}

	return records, nil
}

// NewFakeRecorder creates new fake event recorder with event channel with
// buffer of given size.
func NewFakeRecorder(bufferSize int) *FakeRecorder {
	return &FakeRecorder{
		Events:  make(chan string, bufferSize),
		Message: make(map[string][]*Events),
	}
}

// randomNamespaceName generates a random name based on the basic name.
// Running each ginkgo case in a new namespace with a random name can avoid
// waiting a long time to GC namespace.
func randomNamespaceName(basic string) string {
	return fmt.Sprintf("%s-%s", basic, strconv.FormatInt(rand.Int63(), 16))
}
