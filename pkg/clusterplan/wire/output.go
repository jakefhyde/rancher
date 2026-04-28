package wire

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"k8s.io/client-go/util/jsonpath"
)

// ExtractOutputs reads the system-agent's gzipped applied-output blob
// (a JSON map keyed by instruction name → stdout bytes) and projects it
// into the framework's typed OutputStatus map according to the supplied
// Output declarations.
//
// Output.Source is parsed as "<instructionName>" or
// "<instructionName>:<jsonPath>". The JSONPath dialect is the one
// implemented by k8s.io/client-go/util/jsonpath; missing keys yield the
// empty string rather than an error.
func ExtractOutputs(secretData map[string][]byte, outputs []v1alpha1.Output) (map[string]v1alpha1.OutputStatus, error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	stdoutByName, err := decodeAppliedOutput(secretData[PlanSecretKeyAppliedOutput])
	if err != nil {
		return nil, err
	}
	out := make(map[string]v1alpha1.OutputStatus, len(outputs))
	for _, o := range outputs {
		instName, jp := splitSource(o.Source)
		raw, ok := stdoutByName[instName]
		if !ok {
			// Skip — instruction may not have run yet.
			continue
		}
		val := string(raw)
		if jp != "" {
			projected, err := projectJSONPath(raw, jp)
			if err != nil {
				return nil, fmt.Errorf("output %q (jsonPath %q): %w", o.Name, jp, err)
			}
			val = projected
		}
		out[o.Name] = v1alpha1.OutputStatus{Value: val, Persistent: o.Persistent}
	}
	return out, nil
}

// decodeAppliedOutput gunzips the applied-output blob and returns the
// per-instruction stdout map. Returns an empty map (not an error) when
// the blob is absent or empty — that just means no instructions with
// SaveOutput=true have completed yet.
func decodeAppliedOutput(blob []byte) (map[string][]byte, error) {
	if len(blob) == 0 {
		return map[string][]byte{}, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("wire: gunzip applied-output: %w", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("wire: read applied-output: %w", err)
	}
	out := map[string][]byte{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("wire: parse applied-output: %w", err)
	}
	return out, nil
}

// EncodeAppliedOutput is the inverse of decodeAppliedOutput. Mostly
// used by tests to construct a synthetic agent-side reply.
func EncodeAppliedOutput(stdoutByName map[string][]byte) ([]byte, error) {
	raw, err := json.Marshal(stdoutByName)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// splitSource splits an Output.Source like "name" or "name:.foo.bar" into
// its instruction name and (optional) JSONPath. Empty jsonPath means
// the entire stdout is captured verbatim.
func splitSource(src string) (instructionName, jsonPath string) {
	src = strings.TrimSpace(src)
	if i := strings.Index(src, ":"); i >= 0 {
		return src[:i], src[i+1:]
	}
	return src, ""
}

// projectJSONPath evaluates a JSONPath against JSON-bytes stdout and
// returns the projected string. Uses the same engine the render package
// exposes; reproduced here so the wire package has no dependency on
// pkg/clusterplan/render.
func projectJSONPath(stdout []byte, expr string) (string, error) {
	var doc interface{}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return "", fmt.Errorf("stdout is not JSON: %w", err)
	}
	jp := jsonpath.New("wire").AllowMissingKeys(true)
	wrapped := expr
	if !strings.HasPrefix(wrapped, "{") {
		wrapped = "{" + wrapped + "}"
	}
	if err := jp.Parse(wrapped); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := jp.Execute(&buf, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Compile-time guard so the unused text/template import doesn't get
// pruned — kept here intentionally because future Output.Source forms
// (e.g. plain Go-template projections) are likely to need it.
var _ = template.New
