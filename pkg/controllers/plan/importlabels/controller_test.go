package importlabels

import (
	"reflect"
	"testing"
)

func TestDesiredLabels(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{
			name: "control-plane only",
			in:   map[string]string{StandardControlPlaneLabel: ""},
			want: map[string]string{RancherControlPlaneLabel: "true"},
		},
		{
			name: "etcd only",
			in:   map[string]string{StandardEtcdLabel: ""},
			want: map[string]string{RancherEtcdLabel: "true"},
		},
		{
			name: "both control-plane and etcd",
			in: map[string]string{
				StandardControlPlaneLabel: "",
				StandardEtcdLabel:         "",
			},
			want: map[string]string{
				RancherControlPlaneLabel: "true",
				RancherEtcdLabel:         "true",
			},
		},
		{
			name: "no roles ⇒ worker",
			in:   map[string]string{},
			want: map[string]string{RancherWorkerLabel: "true"},
		},
		{
			name: "irrelevant labels",
			in:   map[string]string{"foo": "bar"},
			want: map[string]string{RancherWorkerLabel: "true"},
		},
		{
			name: "control-plane present, no worker label",
			in:   map[string]string{StandardControlPlaneLabel: ""},
			want: map[string]string{RancherControlPlaneLabel: "true"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DesiredLabels(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestLabelsEqual(t *testing.T) {
	have := map[string]string{"a": "1", "b": "2", "extra": "ignored"}
	want := map[string]string{"a": "1", "b": "2"}
	if !labelsEqual(have, want) {
		t.Errorf("expected equal: have %v, want %v", have, want)
	}
	want["c"] = "3"
	if labelsEqual(have, want) {
		t.Errorf("missing key c should be detected")
	}
}
