package kubectl

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestXxx(t *testing.T) {
	out, err := Opts{Kubeconfig: "/home/tink/.kube/config"}.GetNodeCidrs(context.Background())
	if err != nil {
		t.Fatalf("error getting trusted proxies: %s: out: %v", err, out)
	}
	t.Log(out)
	t.Fail()
}

func TestXxx2(t *testing.T) {
	var trustedProxies []string
	timeout := time.NewTimer(time.Minute)
LOOP:
	for {
		select {
		case <-timeout.C:
			t.Fatal(fmt.Errorf("unable to get node cidrs after 1 minute"))
		default:
		}
		/*
			cmd := "kubectl"
			args := []string{"get", "nodes", "-o", "jsonpath='{.items[*].spec.podCIDR}'"}
			e := exec.CommandContext(context.Background(), cmd, args...)
			e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig)}
			out, err := e.CombinedOutput()
			if err != nil {
				return fmt.Errorf("error getting trusted proxies: %s: out: %v", err, string(out))
			}
			// strip quotes
			trustedProxies = strings.Trim(string(out), "'")
		*/
		cidrs, err := Opts{Kubeconfig: "/home/tink/.kube/config"}.GetNodeCidrs(context.Background())
		if err != nil {
			t.Fatal(fmt.Errorf("error getting node cidrs: %w", err))
		}
		for _, c := range cidrs {
			v, ipnet, _ := net.ParseCIDR(c)
			if v != nil {
				t.Log(v)
				t.Log(ipnet)
				trustedProxies = append(trustedProxies, ipnet.String())
				break LOOP
			}
		}
	}
	t.Log(trustedProxies)
	t.Fail()
}
