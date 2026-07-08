package output

import (
	"encoding/json"
	"io"

	// Registers the family YAML encoder with lib-agent-output so out.Print
	// handles FormatYAML (2-space indent, whole floats rendered as ints, and
	// per-stream colorization through the shared funnel).
	_ "github.com/shhac/lib-agent-cli/yaml"
	out "github.com/shhac/lib-agent-output"
)

// PrintYAMLViaJSON writes data as YAML after a JSON round-trip, so the keys,
// omitempty behavior, and nil handling match the JSON output exactly. Schema
// structs carry json tags that yaml.v3 ignores (it lowercases Go field names),
// so encoding them directly diverges from --format json; routing through JSON
// first keeps the two formats shape-identical.
func PrintYAMLViaJSON(w io.Writer, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return err
	}
	return out.Print(w, normalized, out.FormatYAML, nil)
}
