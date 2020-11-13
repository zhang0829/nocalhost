/*
Copyright 2020 The Nocalhost Authors.
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

package cmds

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	batch "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	cachetools "k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"nocalhost/pkg/nhctl/clientgoutils"
	"nocalhost/pkg/nhctl/tools"
	"nocalhost/pkg/nhctl/utils"
	"os"
	"sort"
	"strings"
	"time"
)

type InstallFlags struct {
	*EnvSettings
	Url           string // resource url
	AppType       string
	ResourcesDir  string
	HelmValueFile string
	ForceInstall  bool
}

var installFlags = InstallFlags{
	EnvSettings: settings,
}

func init() {
	installCmd.Flags().StringVarP(&nameSpace, "namespace", "n", "", "kubernetes namespace")
	installCmd.Flags().StringVarP(&installFlags.Url, "url", "u", "", "resource url")
	installCmd.Flags().StringVarP(&installFlags.ResourcesDir, "dir", "d", "", "the dir of helm package or manifest")
	installCmd.Flags().StringVarP(&installFlags.HelmValueFile, "", "f", "", "helm's Value.yaml")
	installCmd.Flags().StringVarP(&installFlags.AppType, "type", "t", "", "app type: helm or manifest")
	installCmd.Flags().BoolVar(&installFlags.ForceInstall, "force", installFlags.ForceInstall, "force install")
	rootCmd.AddCommand(installCmd)
}

var installCmd = &cobra.Command{
	Use:   "install [NAME]",
	Short: "install k8s application",
	Long:  `install k8s application`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.Errorf("%q requires at least 1 argument\n", cmd.CommandPath())
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if nameSpace == "" {
			fmt.Println("error: please use -n to specify a kubernetes namespace")
			return
		}
		if installFlags.Url == "" {
			fmt.Println("error: please use -u to specify url of git")
			return
		}
		fmt.Println("install application...")
		InstallApplication(args[0])
	},
}

func InstallApplication(applicationName string) {

	var (
		resourcesPath string
		err           error
	)

	// create application dir
	applicationDir := GetApplicationHomeDir(applicationName)
	if _, err = os.Stat(applicationDir); err != nil {
		if os.IsNotExist(err) {
			debug("%s not exists, create application dir", applicationDir)
			utils.Mush(os.Mkdir(applicationDir, 0755))
			utils.Mush(DownloadApplicationToNhctlHome(applicationDir))
		} else {
			panic(err)
		}
	} else if !installFlags.ForceInstall {
		fmt.Printf("%s already exists, please use --force to force it to be reinstalled\n", applicationName)
		return
	} else if installFlags.ForceInstall {
		fmt.Printf("force to reinstall %s\n", applicationName)
		utils.Mush(os.RemoveAll(applicationDir))
		utils.Mush(os.Mkdir(applicationDir, 0755))
		utils.Mush(DownloadApplicationToNhctlHome(applicationDir))
	}

	app, err = NewApplication(applicationName)
	utils.Mush(err)
	config := app.Config

	// install dependence config map

	if installFlags.ResourcesDir != "" {
		resourcesPath = fmt.Sprintf("%s%c%s", applicationDir, os.PathSeparator, installFlags.ResourcesDir)
	} else {
		resourcesPath = fmt.Sprintf("%s%c%s", applicationDir, os.PathSeparator, config.AppConfig.ResourcePath)
	}
	debug("resources path is %s\n", resourcesPath)
	if installFlags.AppType == "" {
		installFlags.AppType = config.AppConfig.Type
		debug("[nocalhost config] app type: %s", config.AppConfig.Type)
	}
	if installFlags.AppType == "helm" {
		params := []string{"upgrade", "--install", "--wait", applicationName, resourcesPath, "--debug"}
		if nameSpace != "" {
			params = append(params, "-n", nameSpace)
		}
		if settings.KubeConfig != "" {
			params = append(params, "--kubeconfig", settings.KubeConfig)
		}
		fmt.Println("install helm application, this may take several minutes, please waiting...")
		output, err := tools.ExecCommand(nil, false, "helm", params...)
		if err != nil {
			printlnErr("fail to install helm app", err)
			return
		}
		debug(output)
		fmt.Printf(`helm app installed, use "helm list -n %s" to get the information of the helm release`+"\n", nameSpace)
	} else if installFlags.AppType == "manifest" {
		excludeFiles := make([]string, 0)
		if config.PreInstall != nil {
			debug("[nocalhost config] reading pre-install hook")
			excludeFiles, err = PreInstall(resourcesPath, config.PreInstall)
			utils.Mush(err)
		}

		// install manifest recursively, don't install pre-install workload again
		InstallManifestRecursively(resourcesPath, excludeFiles)
	} else {
		fmt.Println("unsupported application type, it mush be helm or manifest")
	}
}

func DownloadApplicationToNhctlHome(homePath string) error {
	var (
		err        error
		gitDirName string
	)

	if strings.HasPrefix(installFlags.Url, "https") || strings.HasPrefix(installFlags.Url, "git") || strings.HasPrefix(installFlags.Url, "http") {
		if strings.HasSuffix(installFlags.Url, ".git") {
			gitDirName = installFlags.Url[:len(installFlags.Url)-4]
		} else {
			gitDirName = installFlags.Url
		}
		debug("git dir : " + gitDirName)
		strs := strings.Split(gitDirName, "/")
		gitDirName = strs[len(strs)-1] // todo : for default application anme
		// clone git to homePath
		_, err = tools.ExecCommand(nil, true, "git", "clone", installFlags.Url, homePath)
		if err != nil {
			printlnErr("fail to clone git", err)
			return err
		}
	} else { // todo: for no git url
		fmt.Println("installing ")
		return nil
	}
	return nil
}

func InstallManifestRecursively(dir string, excludeFiles []string) error {

	files, _, err := GetFilesAndDirs(dir)
	if err != nil {
		return err
	}

outer:
	for _, file := range files {
		for _, ex := range excludeFiles {
			if ex == file {
				fmt.Println("ignore file : " + file)
				continue outer
			}
		}
		fmt.Println("create " + file)
		clientUtil, err := clientgoutils.NewClientGoUtils(settings.KubeConfig, 0)
		if err != nil {
			return err
		}
		clientUtil.Create(file, nameSpace, false)
	}
	return err
}

func PreInstall(basePath string, items []*PreInstallItem) ([]string, error) {
	fmt.Println("run pre-install....")

	// sort
	sort.Sort(ComparableItems(items))

	clientUtils, err := clientgoutils.NewClientGoUtils(settings.KubeConfig, 0)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, item := range items {
		fmt.Println(item.Path + " : " + item.Weight)
		files = append(files, basePath+"/"+item.Path)
		// todo check if item.Path is a valid file
		err = clientUtils.Create(basePath+"/"+item.Path, nameSpace, true)
		if err != nil {
			return files, err
		}
	}
	return files, nil
}

func waitUtilReady() error {
	resourceName := ""
	kind := ""
	switch kind {
	case "Job", "Pod": // only wait for job and pod
	default:
		return nil
	}

	selector, err := fields.ParseSelector(fmt.Sprintf("metadata.Name=%s", resourceName))
	if err != nil {
		return err
	}

	restClient, err := GetRestClient()

	lw := cachetools.NewListWatchFromClient(restClient, "Job", nameSpace, selector)
	ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), time.Minute*5)
	defer cancel()
	_, err = watchtools.UntilWithSync(ctx, lw, &unstructured.Unstructured{}, nil, func(e watch.Event) (bool, error) {
		switch e.Type {
		case watch.Added, watch.Modified:
			// For things like a secret or a config map, this is the best indicator
			// we get. We care mostly about jobs, where what we want to see is
			// the status go into a good state. For other types, like ReplicaSet
			// we don't really do anything to support these as hooks.
			switch kind {
			case "Job":
				return waitForJob(e.Object, resourceName)
			case "Pod":
				return waitForPodSuccess(e.Object, resourceName)
			}
			return true, nil
		case watch.Deleted:
			fmt.Printf("Deleted event for %s", resourceName)
			return true, nil
		case watch.Error:
			// Handle error and return with an error.
			fmt.Printf("Error event for %s", resourceName)
			return true, errors.Errorf("failed to deploy %s", resourceName)
		default:
			return false, nil
		}
	})

	return nil
}

func waitForJob(obj runtime.Object, name string) (bool, error) {
	o, ok := obj.(*batch.Job)
	if !ok {
		return true, errors.Errorf("expected %s to be a *batch.Job, got %T", name, obj)
	}

	for _, c := range o.Status.Conditions {
		if c.Type == batch.JobComplete && c.Status == "True" {
			return true, nil
		} else if c.Type == batch.JobFailed && c.Status == "True" {
			return true, errors.Errorf("job failed: %s", c.Reason)
		}
	}

	return false, nil
}

func waitForPodSuccess(obj runtime.Object, name string) (bool, error) {
	o, ok := obj.(*v1.Pod)
	if !ok {
		return true, errors.Errorf("expected %s to be a *v1.Pod, got %T", name, obj)
	}

	switch o.Status.Phase {
	case v1.PodSucceeded:
		fmt.Printf("Pod %s succeeded", o.Name)
		return true, nil
	case v1.PodFailed:
		return true, errors.Errorf("pod %s failed", o.Name)
	case v1.PodPending:
		fmt.Printf("Pod %s pending", o.Name)
	case v1.PodRunning:
		fmt.Printf("Pod %s running", o.Name)
	}
	return false, nil
}