package k8s

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	clientset *kubernetes.Clientset
	dynClient dynamic.Interface
)

func init() {
	err := createK8sClients()

	if err != nil {
		log.Fatal(fmt.Sprintf("unable to create kubernetes clients: %s", err.Error()))
	}
}

func GetConfigMap(nameConfigMap string, namespace string) (*corev1.ConfigMap, error) {
	// Get ConfigMap
	configmap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.Background(),
		nameConfigMap, metav1.GetOptions{})

	if err != nil {
		return nil, err
	}

	return configmap, nil
}

func GetSecret(secretName string, namespace string) (*corev1.Secret, error) {
	// Get Secret
	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.Background(),
		secretName, metav1.GetOptions{})

	if err != nil {
		return nil, err
	}

	return secret, nil
}

func createK8sClients() error {
	var config *rest.Config

	// Creates the in-cluster config
	config, err := rest.InClusterConfig()

	if err != nil {
		// Try with kubeconfig out of the cluster
		home := homedir.HomeDir()

		if len(home) == 0 {
			return errors.New("home dir not found")
		}

		kubeconfig := filepath.Join(home, ".kube", "config")

		// Use the current context in kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)

		if err != nil {
			return err
		}
	}

	// Creates the clientset
	clientset, err = kubernetes.NewForConfig(config)

	if err != nil {
		return err
	}

	// Create dynamic client for creating PipelineRun instances
	dynClient, err = dynamic.NewForConfig(config)

	if err != nil {
		return err
	}

	return nil
}

func CreateObject(k8sApiGroup string, k8sApiVersion string, k8sKind string, namespace string, rawObj string) error {
	resource := schema.GroupVersionResource{Group: k8sApiGroup, Version: k8sApiVersion,
		Resource: strings.ToLower(fmt.Sprintf("%ss", k8sKind))}

	obj := &unstructured.Unstructured{}

	// Decode YAML into unstructured.Unstructured
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	_, _, err := dec.Decode([]byte(rawObj), nil, obj)

	if err != nil {
		return err
	}

	_, err = dynClient.Resource(resource).Namespace(namespace).Create(context.Background(), obj, metav1.CreateOptions{})

	return err
}
