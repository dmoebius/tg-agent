package vm

import (
	"os/exec"
	"testing"
)

func TestParseDomainResources(t *testing.T) {
	for name, tc := range map[string]struct {
		xml        string
		cpus, memB int
	}{
		"virt-install": {`<domain type='kvm'><name>tg-agent</name>
  <memory unit='KiB'>8388608</memory>
  <currentMemory unit='KiB'>8388608</currentMemory>
  <vcpu placement='static'>4</vcpu></domain>`, 4, 8192},
		"current vcpus": {`<domain><memory unit='MiB'>2048</memory><vcpu current='2'>8</vcpu></domain>`, 2, 2048},
		"no unit":       {`<domain><memory>4194304</memory><vcpu>1</vcpu></domain>`, 1, 4096},
	} {
		cpus, mem, err := parseDomainResources(tc.xml)
		if err != nil || cpus != tc.cpus || mem != tc.memB {
			t.Errorf("%s: got %d CPUs, %d MiB, %v; want %d, %d", name, cpus, mem, err, tc.cpus, tc.memB)
		}
	}
	if _, _, err := parseDomainResources(`<domain><memory unit='b'>1</memory><vcpu>1</vcpu></domain>`); err == nil {
		t.Error("unexpected unit: expected error")
	}
	if _, _, err := parseDomainResources(`<domain/>`); err == nil {
		t.Error("empty domain: expected error")
	}
}

func TestParseCapacity(t *testing.T) {
	out := "Capacity:       64424509440\nAllocation:     6512345088\nPhysical:       6512345088\n"
	if b, err := parseCapacity(out); err != nil || b != 60<<30 {
		t.Fatalf("got %d, %v", b, err)
	}
	if _, err := parseCapacity("error: no such disk\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGrowRootFSIsValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if out, err := exec.Command(bash, "-n", "-c", growRootFS).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
