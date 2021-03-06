/*
Copyright 2014 Google Inc. All rights reserved.

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

package client

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	kubeclient "github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubecfg"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"
	"github.com/golang/glog"
)

type KubeConfig struct {
	ServerVersion bool
	PreventSkew   bool
	HttpServer    string
	Config        string
	Selector      string
	UpdatePeriod  time.Duration
	PortSpec      string
	ServicePort   int
	AuthConfig    string
	JSON          bool
	YAML          bool
	Verbose       bool
	Proxy         bool
	WWW           string
	TemplateFile  string
	TemplateStr   string

	Args []string
}

func (c *KubeConfig) Arg(index int) string {
	if index >= len(c.Args) {
		return ""
	}
	return c.Args[index]
}

func usage(name string) string {
	return fmt.Sprintf(`
  Kubernetes REST API:
  %[1]s [OPTIONS] get|list|create|delete|update <%[2]s>[/<id>]

  Manage replication controllers:
  %[1]s [OPTIONS] stop|rm|rollingupdate <controller>
  %[1]s [OPTIONS] run <image> <replicas> <controller>
  %[1]s [OPTIONS] resize <controller> <replicas>
`, name, prettyWireStorage())
}

func prettyWireStorage() string {
	types := kubecfg.SupportedWireStorage()
	sort.Strings(types)
	return strings.Join(types, "|")
}

// readConfig reads and parses pod, replicationController, and service
// configuration files. If any errors log and exit non-zero.
func (c *KubeConfig) readConfig(storage string) []byte {
	if len(c.Config) == 0 {
		glog.Fatal("Need config file (-c)")
	}
	data, err := ioutil.ReadFile(c.Config)
	if err != nil {
		glog.Fatalf("Unable to read %v: %v\n", c.Config, err)
	}
	data, err = kubecfg.ToWireFormat(data, storage)
	if err != nil {
		glog.Fatalf("Error parsing %v as an object for %v: %v\n", c.Config, storage, err)
	}
	if c.Verbose {
		glog.Infof("Parsed config file successfully; sending:\n%v\n", string(data))
	}
	return data
}

func (c *KubeConfig) Run() {
	util.InitLogs()
	defer util.FlushLogs()

	secure := true
	var masterServer string
	if len(c.HttpServer) > 0 {
		masterServer = c.HttpServer
	} else if len(os.Getenv("KUBERNETES_MASTER")) > 0 {
		masterServer = os.Getenv("KUBERNETES_MASTER")
	} else {
		masterServer = "http://localhost:8080"
	}
	parsedURL, err := url.Parse(masterServer)
	if err != nil {
		glog.Fatalf("Unable to parse %v as a URL\n", err)
	}
	if parsedURL.Scheme != "" && parsedURL.Scheme != "https" {
		secure = false
	}

	var auth *kubeclient.AuthInfo
	if secure {
		auth, err = kubecfg.LoadAuthInfo(c.AuthConfig, os.Stdin)
		if err != nil {
			glog.Fatalf("Error loading auth: %v", err)
		}
	}

	client := kubeclient.New(masterServer, auth)

	if c.ServerVersion {
		got, err := client.ServerVersion()
		if err != nil {
			fmt.Printf("Couldn't read version from server: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Server Version: %#v\n", got)
		os.Exit(0)
	}

	if c.PreventSkew {
		got, err := client.ServerVersion()
		if err != nil {
			fmt.Printf("Couldn't read version from server: %v\n", err)
			os.Exit(1)
		}
		if c, s := version.Get(), *got; !reflect.DeepEqual(c, s) {
			fmt.Printf("Server version (%#v) differs from client version (%#v)!\n", s, c)
			os.Exit(1)
		}
	}

	if c.Proxy {
		glog.Info("Starting to serve on localhost:8001")
		server := kubecfg.NewProxyServer(c.WWW, masterServer, auth)
		glog.Fatal(server.Serve())
	}

	method := c.Arg(0)

	matchFound := c.executeAPIRequest(method, client) || c.executeControllerRequest(method, client)
	if matchFound == false {
		glog.Fatalf("Unknown command %s", method)
	}
}

// storagePathFromArg normalizes a path and breaks out the first segment if available
func storagePathFromArg(arg string) (storage, path string, hasSuffix bool) {
	path = strings.Trim(arg, "/")
	segments := strings.SplitN(path, "/", 2)
	storage = segments[0]
	if len(segments) > 1 && segments[1] != "" {
		hasSuffix = true
	}
	return storage, path, hasSuffix
}

//checkStorage returns true if the provided storage is valid
func checkStorage(storage string) bool {
	for _, allowed := range kubecfg.SupportedWireStorage() {
		if allowed == storage {
			return true
		}
	}
	return false
}

func (c *KubeConfig) executeAPIRequest(method string, client *kubeclient.Client) bool {
	storage, path, hasSuffix := storagePathFromArg(c.Arg(1))
	validStorage := checkStorage(storage)
	verb := ""
	setBody := false
	var version uint64
	switch method {
	case "get":
		verb = "GET"
		if !validStorage || !hasSuffix {
			glog.Fatalf("usage: kubecfg [OPTIONS] %s <%s>[/<id>]", method, prettyWireStorage())
		}
	case "list":
		verb = "GET"
		if !validStorage || hasSuffix {
			glog.Fatalf("usage: kubecfg [OPTIONS] %s <%s>", method, prettyWireStorage())
		}
	case "delete":
		verb = "DELETE"
		if !validStorage || !hasSuffix {
			glog.Fatalf("usage: kubecfg [OPTIONS] %s <%s>/<id>", method, prettyWireStorage())
		}
	case "create":
		verb = "POST"
		setBody = true
		if !validStorage || hasSuffix {
			glog.Fatalf("usage: kubecfg [OPTIONS] %s <%s>", method, prettyWireStorage())
		}
	case "update":
		obj, err := client.Verb("GET").Path(path).Do().Get()
		if err != nil {
			glog.Fatalf("error obtaining resource version for update: %v", err)
		}
		jsonBase, err := api.FindJSONBase(obj)
		if err != nil {
			glog.Fatalf("error finding json base for update: %v", err)
		}
		version = jsonBase.ResourceVersion()
		verb = "PUT"
		setBody = true
		if !validStorage || !hasSuffix {
			glog.Fatalf("usage: kubecfg [OPTIONS] %s <%s>/<id>", method, prettyWireStorage())
		}
	default:
		return false
	}

	r := client.Verb(verb).
		Path(path).
		ParseSelector(c.Selector)
	if setBody {
		if version != 0 {
			data := c.readConfig(storage)
			obj, err := api.Decode(data)
			if err != nil {
				glog.Fatalf("error setting resource version: %v", err)
			}
			jsonBase, err := api.FindJSONBase(obj)
			if err != nil {
				glog.Fatalf("error setting resource version: %v", err)
			}
			jsonBase.SetResourceVersion(version)
			data, err = api.Encode(obj)
			if err != nil {
				glog.Fatalf("error setting resource version: %v", err)
			}
			r.Body(data)
		} else {
			r.Body(c.readConfig(storage))
		}
	}
	result := r.Do()
	obj, err := result.Get()
	if err != nil {
		glog.Fatalf("Got request error: %v\n", err)
		return false
	}

	var printer kubecfg.ResourcePrinter
	switch {
	case c.JSON:
		printer = &kubecfg.IdentityPrinter{}
	case c.YAML:
		printer = &kubecfg.YAMLPrinter{}
	case len(c.TemplateFile) > 0 || len(c.TemplateStr) > 0:
		var data []byte
		if len(c.TemplateFile) > 0 {
			var err error
			data, err = ioutil.ReadFile(c.TemplateFile)
			if err != nil {
				glog.Fatalf("Error reading template %s, %v\n", c.TemplateFile, err)
				return false
			}
		} else {
			data = []byte(c.TemplateStr)
		}
		tmpl, err := template.New("output").Parse(string(data))
		if err != nil {
			glog.Fatalf("Error parsing template %s, %v\n", string(data), err)
			return false
		}
		printer = &kubecfg.TemplatePrinter{
			Template: tmpl,
		}
	default:
		printer = &kubecfg.HumanReadablePrinter{}
	}

	if err = printer.PrintObj(obj, os.Stdout); err != nil {
		body, _ := result.Raw()
		glog.Fatalf("Failed to print: %v\nRaw received object:\n%#v\n\nBody received: %v", err, obj, string(body))
	}
	fmt.Print("\n")

	return true
}

func (c *KubeConfig) executeControllerRequest(method string, client *kubeclient.Client) bool {
	parseController := func() string {
		if len(c.Args) != 2 {
			glog.Fatal("usage: kubecfg [OPTIONS] stop|rm|rollingupdate <controller>")
		}
		return c.Arg(1)
	}

	var err error
	switch method {
	case "stop":
		err = kubecfg.StopController(parseController(), client)
	case "rm":
		err = kubecfg.DeleteController(parseController(), client)
	case "rollingupdate":
		err = kubecfg.Update(parseController(), client, c.UpdatePeriod)
	case "run":
		if len(c.Args) != 4 {
			glog.Fatal("usage: kubecfg [OPTIONS] run <image> <replicas> <controller>")
		}
		image := c.Arg(1)
		replicas, err := strconv.Atoi(c.Arg(2))
		name := c.Arg(3)
		if err != nil {
			glog.Fatalf("Error parsing replicas: %v", err)
		}
		err = kubecfg.RunController(image, name, replicas, client, c.PortSpec, c.ServicePort)
	case "resize":
		args := c.Args
		if len(args) < 3 {
			glog.Fatal("usage: kubecfg resize <controller> <replicas>")
		}
		name := args[1]
		replicas, err := strconv.Atoi(args[2])
		if err != nil {
			glog.Fatalf("Error parsing replicas: %v", err)
		}
		err = kubecfg.ResizeController(name, replicas, client)
	default:
		return false
	}
	if err != nil {
		glog.Fatalf("Error: %v", err)
	}
	return true
}
