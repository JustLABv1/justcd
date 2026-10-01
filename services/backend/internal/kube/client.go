package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/justlab/justcd/services/backend/internal/security"
	"github.com/justlab/justcd/services/backend/internal/store"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type Clients struct {
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Mapper    meta.RESTMapper
}

type credentialPayload struct {
	Token   string `json:"token"`
	Content string `json:"content"`
}

func ForBinding(ctx context.Context, db *store.Store, key []byte, cluster store.Cluster, credentialID *string, clusterScope bool) (*Clients, error) {
	if AgentEnabled(ctx, db, cluster.ID) {
		return nil, errors.New("agent connections require a workspace binding")
	}
	if credentialID == nil {
		if clusterScope {
			credentialID = cluster.ClusterScopeCredential
		} else {
			credentialID = cluster.DefaultCredentialID
		}
	}
	if credentialID == nil {
		return nil, errors.New("no Kubernetes credential is configured for this binding")
	}
	credential, err := db.CredentialByID(ctx, *credentialID)
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes credential: %w", err)
	}
	if credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now()) {
		return nil, errors.New("Kubernetes credential has expired")
	}
	if credential.Kind != "kubernetes-token" && credential.Kind != "kubeconfig" {
		return nil, errors.New("credential is not a Kubernetes credential")
	}
	plain, err := security.Decrypt(key, credential.Cipher, "credential:"+credential.ID)
	if err != nil {
		return nil, err
	}
	var payload credentialPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, errors.New("Kubernetes credential payload is invalid")
	}
	var config *rest.Config
	if credential.Kind == "kubernetes-token" {
		if payload.Token == "" {
			return nil, errors.New("Kubernetes token is empty")
		}
		config = &rest.Config{Host: cluster.APIServer, BearerToken: payload.Token, TLSClientConfig: rest.TLSClientConfig{CAData: cluster.CAData, Insecure: cluster.InsecureSkipVerify}}
	} else {
		config, err = staticKubeconfig(payload.Content, cluster)
		if err != nil {
			return nil, err
		}
	}
	config.UserAgent = "JustCD/1.0"
	config.Timeout = 30 * time.Second
	return clientsForConfig(config)
}

func clientsForConfig(config *rest.Config) (*Clients, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))
	return &Clients{Dynamic: dynamicClient, Discovery: discoveryClient, Mapper: mapper}, nil
}

func staticKubeconfig(content string, cluster store.Cluster) (*rest.Config, error) {
	if content == "" {
		return nil, errors.New("kubeconfig is empty")
	}
	raw, err := clientcmd.Load([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	current, ok := raw.Contexts[raw.CurrentContext]
	if !ok || current == nil {
		return nil, errors.New("kubeconfig current-context is missing")
	}
	selectedCluster, ok := raw.Clusters[current.Cluster]
	if !ok || selectedCluster == nil {
		return nil, errors.New("kubeconfig cluster is missing")
	}
	user, ok := raw.AuthInfos[current.AuthInfo]
	if !ok || user == nil {
		return nil, errors.New("kubeconfig user is missing")
	}
	if user.Exec != nil || user.AuthProvider != nil || user.TokenFile != "" || user.Impersonate != "" {
		return nil, errors.New("kubeconfig exec, auth-provider, token-file, and impersonation credentials are not supported")
	}
	config := &rest.Config{
		Host:            cluster.APIServer,
		BearerToken:     user.Token,
		Username:        user.Username,
		Password:        user.Password,
		BearerTokenFile: "",
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   cluster.CAData,
			Insecure: cluster.InsecureSkipVerify,
			CertData: user.ClientCertificateData,
			KeyData:  user.ClientKeyData,
		},
	}
	if config.TLSClientConfig.CAData == nil {
		config.TLSClientConfig.CAData = selectedCluster.CertificateAuthorityData
	}
	if config.BearerToken == "" && config.Username == "" && len(config.TLSClientConfig.CertData) == 0 {
		return nil, errors.New("kubeconfig contains no supported authentication method")
	}
	return config, nil
}
