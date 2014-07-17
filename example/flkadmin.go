package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/jasonmoo/goamz/aws"
	"github.com/jasonmoo/goamz/ec2"

	"prevoty/pd"
)

const (
	DefaultConcurrency = 4

	Instance_Name             = "flk-node"
	Instance_ClassName        = "prod-flk"
	Instance_Ami              = "ami-f34032c3" // ubuntu 14.04 default
	Instance_Type             = "m1.small"
	Instance_AvailabilityZone = "us-west-2b"

	Instance_User    = "ubuntu"
	Instance_KeyName = "flk-key"
	Instance_PubKey  = "flk-key.pem"
)

var (
	Instance_Region         = aws.USWest2
	Instance_SecurityGroups = []ec2.SecurityGroup{
		ec2.SecurityGroup{Name: "prod-flk"},
	}

	// global ec2 region object
	region *ec2.EC2

	// global pool of actionable servers
	pool *pd.Pool

	// default actions
	run          = flag.String("run", "", "command to run on all nodes")
	setup        = flag.Bool("setup", false, "setup server with required packages")
	deploy       = flag.Bool("deploy", false, "deploy the binary/confs/deps")
	rollback     = flag.Bool("rollback", false, "rollback the previous deploy")
	restart      = flag.Bool("restart", false, "restart the app")
	open_port    = flag.Int("open_port", 0, "open a port on the security group")
	close_port   = flag.Int("close_port", 0, "close a port on the security group")
	add_node     = flag.Int("add_node", 0, "add a node to current config")
	remove_node  = flag.Int("remove_node", 0, "remove a node from current pool")
	create_image = flag.String("create_image", "", "create a new ami from an existing server.  value is the image description")

	instance_type     = flag.String("instance_type", Instance_Type, "instance type to launch")
	availability_zone = flag.String("availability_zone", Instance_AvailabilityZone, "az to operate on")
)

func init() {

	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()

	region = ec2.New(aws.Auth{
		AccessKey: "XXXXXXX",
		SecretKey: "XXXXXXX",
	}, Instance_Region)

	log.Println("pd starting up...")
	pd.PrintInstances(region, Instance_ClassName)

	servers := pd.GetPublicDNS(region, Instance_ClassName)

	pool = pd.NewPool(Instance_User, Instance_PubKey, servers, DefaultConcurrency)

}

func main() {

	defer log.Println("done.")

	switch {

	case *run != "":
		pool.WaitForPort(22)
		pd.Must(pool.Run(*run))

	case *open_port > 0:
		pd.OpenPort(region, Instance_ClassName, *open_port)

	case *close_port > 0:
		pd.ClosePort(region, Instance_ClassName, *close_port)

	case *setup:
		log.Println("Setting up...")
		pool.WaitForPort(22)
		log.Println("sleeping for 30 sec to ensure boot...")
		time.Sleep(30 * time.Second)
		pd.Must(pool.Sudo("echo 'debconf debconf/frontend select Noninteractive' | debconf-set-selections"))
		pd.MustShort(pool.Sudo("apt-get -qq -y update"))
		pd.MustShort(pool.Sudo("apt-get -qq -y dist-upgrade"))
		pd.MustShort(pool.Sudo("apt-get -qq -y install git screen rsync vim curl bzip2 psmisc iftop htop lsof strace"))
		pd.Must(pool.Sudo("mkdir -p /var/flk"))
		pd.Must(pool.Sudo("chown -R %s /etc/init /var/flk", Instance_User))
		pd.Must(pool.Rsync("-az", "flk.conf", "/etc/init/flk.conf"))
		pd.Must(pool.Sudo("initctl reload-configuration"))

	case *deploy:
		// build the target binary
		pd.BuildGoBinary("$GOPATH/src/github.com/jasonmoo/flk", "flk", "linux", "amd64")

		// push the new conf
		pool.WaitForPort(22)
		pd.Must(pool.Rsync("-az", "flk.conf", "/etc/init/flk.conf"))

		// push the new binary
		pd.Must(pool.Rsync("-az", "$GOPATH/src/github.com/jasonmoo/flk/flk", "/var/flk/flk.new"))

		// swap the new binary with the old and restart
		// touch ensures file on first deploy
		pd.Must(pool.Sudo(`
			touch /var/flk/flk                    &&
			mv /var/flk/flk     /var/flk/flk.old  &&
			mv /var/flk/flk.new /var/flk/flk      &&
			(restart flk || start flk)
		`))

	case *rollback:
		// swap the binaries and restart
		pd.Must(pool.Sudo(`
			mv /var/flk/flk.old  /var/flk/flk.new  &&
			mv /var/flk/flk      /var/flk/flk.old  &&
			mv /var/flk/flk.new  /var/flk/flk      &&
			restart flk
		`))

	case *restart:
		pd.Must(pool.Sudo("restart flk"))

	case *add_node > 0:
		pd.AddNode(region, Instance_Name, Instance_ClassName, &ec2.RunInstancesOptions{
			ImageId:          Instance_Ami,
			MinCount:         *add_node,
			MaxCount:         *add_node,
			AvailabilityZone: *availability_zone,
			KeyName:          Instance_KeyName,
			InstanceType:     *instance_type,
			SecurityGroups:   Instance_SecurityGroups,
		})

	case *remove_node > 0:
		pd.RemoveNode(region, Instance_ClassName, *remove_node)

	case *create_image != "":
		pd.CreateImage(region, Instance_ClassName, *create_image)

	default:
		fmt.Println("Usage: ")
		flag.PrintDefaults()

	}

}
