// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

const kubeconfigSample = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: prod
users:
- name: admin
  user:
    client-certificate-data: LS0tRkFLRS1DRVJULS0t
    client-key-data: LS0tRkFLRS1LRVktLS0=
`

func TestNoKubeconfigInRepo_ClientKeyDataBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "deploy/creds.yaml", kubeconfigSample)
	res := NoKubeconfigInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Line == 0 {
		t.Fatalf("expected line-anchored client-key-data hit: %+v", res.Hits)
	}
}

func TestNoKubeconfigInRepo_ConventionalFilenameBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "ops/kubeconfig", "apiVersion: v1\nkind: Config\nclusters:\n- name: c\nusers:\n- name: u\n")
	res := NoKubeconfigInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoKubeconfigInRepo_UnrelatedYamlPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "k8s/deployment.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n")
	piiWrite(t, root, "docs/kubeconfig.md", "how to fetch your kubeconfig from the cluster\n")
	res := NoKubeconfigInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoKubeconfigInRepo_DisableLineMarkerHonored(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "docs/example.yaml",
		"# appframes:disable-next-line security/no-kubeconfig-in-repo\nclient-key-data: RkFLRQ==\n")
	res := NoKubeconfigInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
