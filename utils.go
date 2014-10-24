package pd

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jasonmoo/goamz/ec2"
)

var (
	// for memoizing
	InstanceCatalog = make(map[string][]ec2.Instance)
	openPorts       = make(map[string]bool)
)

func MustShort(data interface{}, err error) {
	data_string := fmt.Sprintf("%s", data)
	if len(data_string) > 80 {
		data_string = strings.Replace(data_string[:38]+"...."+data_string[len(data_string)-38:], "\n", "", -1)
	}
	if len(data_string) > 0 {
		log.Println(data_string)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func Must(data interface{}, err error) {
	if entry := fmt.Sprintf("%s", data); len(entry) > 0 {
		log.Println(entry)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func PrintInstances(region *ec2.EC2, class string) {

	instances := FindInstances(region, class)

	if len(instances) == 0 {
		fmt.Fprintln(os.Stderr, "0 active servers")
		return
	}

	w := new(tabwriter.Writer)
	w.Init(os.Stderr, 5, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Public DNS\tSize\tAvailability Zone\tStart Time")

	for _, i := range instances {
		start_time, _ := time.Parse(time.RFC3339, i.LaunchTime)
		fmt.Fprintln(w, strings.Join([]string{i.DNSName, i.InstanceType, i.AvailabilityZone, start_time.Format(time.UnixDate)}, "\t"))
	}

	fmt.Fprintln(w)
	w.Flush()

}

func FindInstances(region *ec2.EC2, class string) []ec2.Instance {

	// memoize
	if r, exists := InstanceCatalog[class]; exists {
		return r
	}

	resp, err := region.DescribeInstances(nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	instances := make([]ec2.Instance, 0)

	for _, r := range resp.Reservations {
		for _, i := range r.Instances {
			if i.State.Name == "running" {
				for _, tag := range i.Tags {
					if tag.Key == "Class" && tag.Value == class {
						instances = append(instances, i)
					}
				}
			}
		}
	}

	return instances
}

func GetPrivateIPs(region *ec2.EC2, class string) []string {
	var ips []string
	for _, i := range FindInstances(region, class) {
		if i.PrivateIPAddress != "" {
			ips = append(ips, i.PrivateIPAddress)
		}
	}
	return ips
}
func GetPublicDNS(region *ec2.EC2, class string) []string {
	var dns []string
	for _, i := range FindInstances(region, class) {
		if i.DNSName != "" {
			dns = append(dns, i.DNSName)
		}
	}
	return dns
}

func BuildGoBinary(source_path, dest_path, goos, goarch string) {
	log.Printf("Building %s/%s binary", goos, goarch)
	source_path = os.ExpandEnv(source_path)
	r, err := Local(fmt.Sprintf("cd %s && GOOS=%s GOARCH=%s go build -o %s", source_path, goos, goarch, dest_path))
	if err != nil {
		log.Fatal(err, string(r))
	}
}

func OpenPort(region *ec2.EC2, class string, port int) {

	log.Printf("Opening port %d", port)

	instances := FindInstances(region, class)

	if len(instances) == 0 {
		log.Fatal("Can't open port, zero instances in this class")
	}

	ipperm := []ec2.IPPerm{ec2.IPPerm{
		Protocol:  "tcp",
		FromPort:  port,
		ToPort:    port,
		SourceIPs: []string{"0.0.0.0/0"},
		// SourceGroups :[]UserSecurityGroup `xml:"groups>item"`
	}}

	_, err := region.AuthorizeSecurityGroup(instances[0].SecurityGroups[0], ipperm)
	if err != nil {
		log.Fatal(err)
	}

	WaitForPort(region, class, port)
}

func OpenPortRange(region *ec2.EC2, class string, from, to int) {

	log.Printf("Opening port range %d-%d", from, to)

	instances := FindInstances(region, class)

	if len(instances) == 0 {
		log.Fatal("Can't open port range, zero instances in this class")
	}

	ipperm := []ec2.IPPerm{ec2.IPPerm{
		Protocol:  "tcp",
		FromPort:  from,
		ToPort:    to,
		SourceIPs: []string{"0.0.0.0/0"},
		// SourceGroups :[]UserSecurityGroup `xml:"groups>item"`
	}}

	_, err := region.AuthorizeSecurityGroup(instances[0].SecurityGroups[0], ipperm)
	if err != nil {
		log.Fatal(err)
	}

}

func ClosePort(region *ec2.EC2, class string, port int) {

	log.Printf("Closing port %d", port)

	instances := FindInstances(region, class)

	if len(instances) == 0 {
		log.Fatal("Can't close port, zero instances in this class")
	}

	ipperm := []ec2.IPPerm{ec2.IPPerm{
		Protocol:  "tcp",
		FromPort:  port,
		ToPort:    port,
		SourceIPs: []string{"0.0.0.0/0"},
		// SourceGroups :[]UserSecurityGroup `xml:"groups>item"`
	}}

	_, err := region.RevokeSecurityGroup(instances[0].SecurityGroups[0], ipperm)
	if err != nil {
		log.Fatal(err)
	}

}

func ClosePortRange(region *ec2.EC2, class string, from, to int) {

	log.Printf("Closing port range %d-%d", from, to)

	instances := FindInstances(region, class)

	if len(instances) == 0 {
		log.Fatal("Can't open port range, zero instances in this class")
	}

	ipperm := []ec2.IPPerm{ec2.IPPerm{
		Protocol:  "tcp",
		FromPort:  from,
		ToPort:    to,
		SourceIPs: []string{"0.0.0.0/0"},
		// SourceGroups :[]UserSecurityGroup `xml:"groups>item"`
	}}

	_, err := region.RevokeSecurityGroup(instances[0].SecurityGroups[0], ipperm)
	if err != nil {
		log.Fatal(err)
	}

}

func WaitForPort(region *ec2.EC2, class string, port int) {

	log.Printf("Waiting for port %d to open...", port)

	for _, server := range GetPublicDNS(region, class) {

		server = fmt.Sprintf("%s:%d", server, port)

		for !openPorts[server] {

			c, err := net.DialTimeout("tcp4", server, 2*time.Second)
			if err != nil {
				fmt.Print(".")
				time.Sleep(2 * time.Second)
				continue
			}

			c.Close()
			openPorts[server] = true
			fmt.Println()

		}

		log.Printf("%s OPEN", server)

	}

}

func WaitForState(region *ec2.EC2, instances []ec2.Instance, state string) error {
	log.Printf("Waiting for state: %s on node(s)...", state)
	for _, i := range instances {
		first := true
		for {
			r, err := region.DescribeInstances([]string{i.InstanceId}, nil)
			if err != nil {
				return err
			}
			if r.Reservations[0].Instances[0].State.Name == state {
				if !first {
					fmt.Println()
				}
				log.Printf(" -> %s %s", i.InstanceId, state)
				break
			}
			fmt.Print(".")
			time.Sleep(2 * time.Second)
			first = false
		}
	}
	return nil
}

func AddNode(region *ec2.EC2, name, class_name string, options *ec2.RunInstancesOptions) (instance_ids []string) {

	log.Printf("Adding %s/%s node(s)", name, class_name)

	start := time.Now()
	res, err := region.RunInstances(options)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Adding tags...")

	instance_ids = make([]string, len(res.Instances))

	for i, instance := range res.Instances {
		instance_ids[i] = instance.InstanceId
	}

	_, err = region.CreateTags(instance_ids, []ec2.Tag{
		ec2.Tag{Key: "Name", Value: name},
		ec2.Tag{Key: "Class", Value: class_name},
	})
	if err != nil {
		log.Fatal(err)
	}

	WaitForState(region, res.Instances, "running")

	log.Printf("New node(s) added in %s", time.Since(start))

	return

}

func RemoveNode(region *ec2.EC2, class_name string, ct int) {
	log.Printf("Removing %d node(s) in %s", ct, class_name)

	instances := FindInstances(region, class_name)

	if ct < len(instances) {
		instances = instances[:ct]
	}

	instance_ids := make([]string, len(instances))

	for i, instance := range instances {
		instance_ids[i] = instance.InstanceId
	}

	_, err := region.TerminateInstances(instance_ids)
	if err != nil {
		log.Fatal(err)
	}

	WaitForState(region, instances, "terminated")
}

func CreateImage(region *ec2.EC2, class_name, description string) {

	instances := FindInstances(region, class_name)

	if len(instances) < 1 {
		log.Fatal("no instances running to snapshot")
	}

	ts := strings.NewReplacer(
		" ", ".",
		":", ".",
		",", "",
		"-", ".",
		"+", ".",
	).Replace(time.Now().Format(time.RFC1123Z))

	name := fmt.Sprintf("%s_%s", class_name, ts)

	r, err := region.CreateImage(instances[0].InstanceId, name, description)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Ami requested")

	for {
		resp, err := region.Images([]string{r.ImageId}, nil)
		if err != nil {
			log.Fatal(err)
		}
		if resp.Images[0].State == "available" {
			break
		}
		fmt.Print(".")
		time.Sleep(2 * time.Second)
	}

	fmt.Println()
	log.Println("Ami available", r.ImageId)
}

func AttachEBSVolume(region *ec2.EC2, volume_id, instance_id, device string) {

	log.Printf("Mounting %s on %s...", volume_id, instance_id)

	_, err := region.AttachVolume(volume_id, instance_id, device)
	if err != nil {
		if strings.Contains(err.Error(), "VolumeInUse") {
			log.Println("Volume already mounted")
			return
		}
		log.Fatal(err)
	}

CHECK:
	i, err := region.DescribeInstances([]string{instance_id}, nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range i.Reservations[0].Instances[0].BlockDevices {
		if d.EBS.VolumeId == volume_id && d.EBS.Status != "attached" {
			fmt.Print(".")
			time.Sleep(2 * time.Second)
			goto CHECK
		}
	}

	fmt.Println()
	log.Printf("Volume %s mounted on %s", volume_id, instance_id)

}

func PHPSyntaxCheck(path string) ([]byte, error) {
	path = os.ExpandEnv(path)
	log.Printf("Checking syntax on %s", path)
	return Local("find %s -type f | egrep .php$ | xargs -P 8 -I {} php -l {} > /dev/null", path)
}
