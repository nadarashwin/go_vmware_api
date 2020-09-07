package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"

	"github.com/spf13/pflag"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

var (
	hostname, username, password, command, datastore string
	warning                                          int
	critical                                         int
)

type resource struct {
	name string
	statistics
}

type statistics struct {
	total float64
	free  float64
}

func (r *resource) freePer() string {
	// return r.free / r.total * 100
	return fmt.Sprintf("%.2f", (r.free / r.total * 100))
}

func init() {
	pflag.StringVarP(&hostname, "hostname", "h", "", "ESXi hostname to query")
	pflag.StringVarP(&username, "username", "u", "", "Username to connect with.")
	pflag.StringVarP(&password, "password", "p", "", "Password to use with the username.")
	pflag.StringVarP(&command, "command", "l", "", "Specify command type (CPU, MEM, VMFS)")
	pflag.StringVarP(&datastore, "datastore", "s", "", "Storage name")
	pflag.IntVarP(&warning, "warning", "w", 85, "Warning Threshold")
	pflag.IntVarP(&critical, "critical", "c", 90, "Critical Threshold")
}

func getKeys(k map[string]string) []string {
	keys := make([]string, 0, len(k))
	for k := range k {
		keys = append(keys, k)
	}
	return keys
}

func urlCheck(h string) string {
	matched, _ := regexp.MatchString("^.*://.*$", h)
	fmt.Println(matched)
	if !matched {
		return fmt.Sprintf("https://%s/sdk", h)
	}
	return h
}

func main() {
	pflag.Parse()
	err := checkRequiredOptions()
	if err != nil {
		pflag.Usage()
		log.Fatal(err)
	}
	commandChoices := map[string]string{"MEM": "HostSystem", "CPU": "HostSystem", "VMFS": "Datastore"}
	if _, validChoice := commandChoices[command]; !validChoice {
		fmt.Printf("valid choices %v\n", getKeys(commandChoices))
		//fmt.Println(getKeys(commandChoices))
		//pflag.Usage()
		os.Exit(2)
	}

	if command == "VMFS" && datastore == "" {
		fmt.Printf("pass storage name for %s option using (-s | --datastore)\n", command)
		pflag.Usage()
		os.Exit(2)
	}

	fmt.Println(hostname, username, password, command, warning, critical, datastore)
	fmt.Println(urlCheck(hostname))

	ctx := context.Background()
	u, _ := url.Parse(urlCheck(hostname))

	u.User = url.UserPassword(username, password)

	fmt.Println(u.String())

	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		log.Fatal(err)
	}

	m := view.NewManager(c.Client)

	v, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{commandChoices[command]}, true)
	//v, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		log.Fatal(err)
	}

	defer v.Destroy(ctx)

	var hss []mo.HostSystem
	var ds []mo.Datastore

	e := &resource{}

	if command == "VMFS" {
		err = v.Retrieve(ctx, []string{commandChoices[command]}, []string{"name", "summary"}, &ds)
		if err != nil {
			log.Fatal(err)
		}

		for _, host := range ds {
			fmt.Println(host.Name)
			if datastore == host.Name {
				e = datastoreStats(host)
				break
			}
		}
		if e.name == "" {
			fmt.Printf("No datastore with name %s found.\n", datastore)
			os.Exit(1)
		}

	} else {
		err = v.Retrieve(ctx, []string{commandChoices[command]}, []string{"name", "summary"}, &hss)
		if err != nil {
			log.Fatal(err)
		}

		for _, host := range hss {
			fmt.Println(host.Name)
			switch command {
			case "CPU":
				e = cpuStats(host)
			case "MEM":
				e = memStats(host)
			}
		}
	}

	fmt.Println(e.name)
	fmt.Printf("total  %f \n free %f \n remaining %s\n", e.total, e.free, e.freePer())

}

func datastoreStats(ds mo.Datastore) *resource {
	return (&resource{
		name: ds.Summary.Name,
		statistics: statistics{
			total: float64(ds.Summary.Capacity),
			free:  float64(ds.Summary.FreeSpace),
		},
	})
}

func cpuStats(host mo.HostSystem) *resource {
	return (&resource{
		name: host.Name,
		statistics: statistics{
			total: float64(host.Summary.Hardware.CpuMhz) * float64(host.Summary.Hardware.NumCpuCores),
			free:  (float64(host.Summary.Hardware.CpuMhz) * float64(host.Summary.Hardware.NumCpuCores)) - float64(host.Summary.QuickStats.OverallCpuUsage),
		},
	})
}

func memStats(host mo.HostSystem) *resource {
	return (&resource{
		name: host.Name,
		statistics: statistics{
			total: float64(host.Summary.Hardware.MemorySize) / 1024 / 1024,
			free:  (float64(host.Summary.Hardware.MemorySize) / 1024 / 1024) - float64(host.Summary.QuickStats.OverallMemoryUsage),
		},
	})
}

func checkRequiredOptions() error {
	switch {
	case hostname == "":
		return fmt.Errorf("Hostname is required")
	case warning == 0:
		return fmt.Errorf("Must supply the warning percentage")
	case critical == 0:
		return fmt.Errorf("Must supply the critical percentage")
	case username == "":
		return fmt.Errorf("Must supply the username")
	case password == "":
		return fmt.Errorf("Must supply the password")
	case command == "":
		return fmt.Errorf("Must supply the command")
	}
	return nil

}
