package kubernetes

import (
	"fmt"
	"github.com/abiosoft/colima/cli"
	"github.com/abiosoft/colima/config"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const kubeconfigKey = "kubeconfig"

func (c kubernetesRuntime) provisionKubeconfig() error {
	provisioned, _ := strconv.ParseBool(c.guest.Get(kubeconfigKey))
	if provisioned {
		return nil
	}

	a := c.Init()

	a.Stage("updating kubeconfig")

	// ensure host kube directory exists
	hostHome := c.host.Env("HOME")
	if hostHome == "" {
		return fmt.Errorf("error retrieving home directory on host")
	}

	hostKubeDir := filepath.Join(hostHome, ".kube")
	a.Add(func() error {
		return c.host.Run("mkdir", "-p", filepath.Join(hostKubeDir, "."+config.Profile()))
	})

	kubeconfFile := filepath.Join(hostKubeDir, "config")
	tmpkubeconfFile := filepath.Join(hostKubeDir, "."+config.Profile(), "colima-temp")

	// manipulate in VM and save to host
	a.Add(func() error {
		kubeconfig, err := c.guest.RunOutput("cat", "/etc/rancher/k3s/k3s.yaml")
		if err != nil {
			return err
		}
		// replace name
		kubeconfig = strings.ReplaceAll(kubeconfig, ": default", ": "+config.Profile())

		// save on the host
		return c.host.Write(tmpkubeconfFile, kubeconfig)
	})

	// merge on host
	a.Add(func() (err error) {
		// prepare new host with right env var.
		envVar := fmt.Sprintf("KUBECONFIG=%s:%s", kubeconfFile, tmpkubeconfFile)
		host := c.host.WithEnv(envVar)

		// get merged config
		kubeconfig, err := host.RunOutput("kubectl", "config", "view", "--raw")
		if err != nil {
			return err
		}

		// save
		return host.Write(tmpkubeconfFile, kubeconfig)
	})

	// backup current settings and save new config
	a.Add(func() error {
		// backup existing file if exists
		if stat, err := c.host.Stat(kubeconfFile); err == nil && !stat.IsDir() {
			backup := filepath.Join(filepath.Dir(tmpkubeconfFile), fmt.Sprintf("config-bak-%d", time.Now().Unix()))
			if err := c.host.Run("cp", kubeconfFile, backup); err != nil {
				return fmt.Errorf("error backing up kubeconfig: %w", err)
			}
		}
		// save new config
		if err := c.host.Run("cp", tmpkubeconfFile, kubeconfFile); err != nil {
			return fmt.Errorf("error updating kubeconfig: %w", err)
		}

		return nil
	})

	// set new context
	a.Add(func() error {
		return c.host.RunInteractive("kubectl", "config", "use-context", config.Profile())
	})

	// save settings
	a.Add(func() error {
		return c.guest.Set(kubeconfigKey, "true")
	})

	return a.Exec()
}
func (c kubernetesRuntime) teardownKubeconfig(a *cli.ActiveCommandChain) {
	a.Stage("reverting kubeconfig")

	a.Add(func() error {
		return c.host.Run("kubectl", "config", "unset", "users."+config.Profile())
	})
	a.Add(func() error {
		return c.host.Run("kubectl", "config", "unset", "contexts."+config.Profile())
	})
	a.Add(func() error {
		return c.host.Run("kubectl", "config", "unset", "clusters."+config.Profile())
	})
}
