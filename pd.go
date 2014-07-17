package pd

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type (
	Pool struct {
		Servers []string
		User    string
		PubKey  string

		output     chan []byte
		workers    chan struct{}
		open_ports map[string]bool
	}
)

var formattingChars = regexp.MustCompile("[ \t\n]+")

func strip(input string) string {
	// this will destroy unescaped formatting chars in a quoted string
	return strings.TrimSpace(formattingChars.ReplaceAllLiteralString(input, " "))
}

func NewPool(user, pub_key string, servers []string, concurrency int) *Pool {
	return &Pool{
		Servers: servers,
		User:    user,
		PubKey:  pub_key,

		output:  make(chan []byte, concurrency),
		workers: make(chan struct{}, concurrency),

		open_ports: make(map[string]bool),
	}
}

func Local(commandf string, params ...interface{}) ([]byte, error) {
	return exec.Command("sh", "-c", fmt.Sprintf(commandf, params...)).CombinedOutput()
}

func (p *Pool) execute(description string, cmd ...string) (cmd_out []byte, cmd_err error) {

	log.Printf("Executing %v", description)

	var buf bytes.Buffer

	for _, server := range p.Servers {

		// make a copy of the cmd and replace all instances of
		// {server} with the server dns
		server_cmd := make([]string, len(cmd))
		for i, part := range cmd {
			server_cmd[i] = strings.Replace(part, "{server}", server, -1)
		}

		go func(server string, cmd []string) {

			p.workers <- struct{}{}

			start := time.Now()

			output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
			if err != nil {
				log.Printf(" -> %s cmd error: %s", server, err)
				cmd_err = err
			} else {
				log.Printf(" -> %s completed in %s", server[:strings.IndexByte(server, '.')], time.Since(start))
			}

			p.output <- output

			<-p.workers

		}(server, server_cmd)

	}

	for i := 0; i < len(p.Servers); i++ {
		buf.Write(<-p.output)
	}

	return buf.Bytes(), cmd_err

}

func (p *Pool) Rsync(flags, local_dsn, remote_dsn string) (cmd_out []byte, err error) {

	local_dsn = os.ExpandEnv(local_dsn)
	remote_dsn = fmt.Sprintf("%s@{server}:%s", p.User, remote_dsn)
	rsync_ssh := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -i %q", p.PubKey)

	return p.execute("rsync "+local_dsn, "rsync", flags, "--inplace", "-e", rsync_ssh, local_dsn, remote_dsn)

}

func (p *Pool) RsyncOptions(flags []string, local_dsn, remote_dsn string) (cmd_out []byte, err error) {

	local_dsn = os.ExpandEnv(local_dsn)
	remote_dsn = fmt.Sprintf("%s@{server}:%s", p.User, remote_dsn)
	rsync_ssh := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -i %q", p.PubKey)

	cmd := append([]string{"rsync"}, append(flags, "-e", rsync_ssh, local_dsn, remote_dsn)...)

	return p.execute("rsync "+local_dsn, cmd...)

}

func (p *Pool) RsyncDelete(flags, local_dsn, remote_dsn string) (cmd_out []byte, err error) {

	local_dsn = os.ExpandEnv(local_dsn)
	remote_dsn = fmt.Sprintf("%s@{server}:%s", p.User, remote_dsn)
	rsync_ssh := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -i %q", p.PubKey)

	return p.execute("rsync "+local_dsn, "rsync", flags, "--inplace", "--delete", "-e", rsync_ssh, local_dsn, remote_dsn)

}

func (p *Pool) Run(commandf string, params ...interface{}) (cmd_out []byte, err error) {

	cmd := fmt.Sprintf("sh -c %q", strip(fmt.Sprintf(commandf, params...)))

	return p.execute(cmd, "ssh", "-o StrictHostKeyChecking=no", "-i", p.PubKey, p.User+"@{server}", cmd)

}

func (p *Pool) Sudo(commandf string, params ...interface{}) (cmd_out []byte, err error) {

	cmd := fmt.Sprintf("sudo sh -c %q", strip(fmt.Sprintf(commandf, params...)))

	return p.execute(cmd, "ssh", "-o StrictHostKeyChecking=no", "-i", p.PubKey, p.User+"@{server}", cmd)

}

func (p *Pool) WaitForPort(port int) {

	log.Printf("Waiting for port %d to open...", port)

	for _, server := range p.Servers {

		server = fmt.Sprintf("%s:%d", server, port)

		for !p.open_ports[server] {

			c, err := net.DialTimeout("tcp4", server, 2*time.Second)
			if err != nil {
				fmt.Print(".")
				time.Sleep(2 * time.Second)
				continue
			}

			c.Close()
			p.open_ports[server] = true
			fmt.Println()

		}

		log.Printf("%s OPEN", server)

	}

}
