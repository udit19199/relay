package executor

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreexecutor "github.com/udit19199/relay/internal/service/executor"
)

const testClaudeCAISSample = "CAISqwIKiAEIEBgCKkBHRlRBsNiptQUWfPoOhuQKwi5LnncZVO9bB5jqOs76D7uBtgktML0zqJtNmLHXHHcgD6lk4MQu4QBXzFd1lbC3Mg5jbGF1ZGUtZmFibGUtNTgBQgh0aGlua2luZ1okZDk3NDM5NzUtNGJiMC00OTM2LTllMjgtZDViMGQyMWJkYzQ4EgxCGh+XVFFFeySAjtAaDL/A1LltGu6MMJ+eXSIwsN0oBpDrqLv22UBfkMnTotnIbkvkOyb9xZHgigG6OZVHaI3gThm+maLKmgO5PrFLKlDFYp+YZksy/wKwszJlnLTPzAK+NUlfzagOE1ymtZTXhAYK260XyFYmg/te/C231+Fr/hoX+EJoUBnrn0gD7hqMISOT+TaFEuOXYsN517GfaxgB"

const executorPatchRequest = `{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"apply_patch","format":{"type":"grammar","definition":"start: patch"}}]}],"input":"patch a file"}`
const executorPatchInput = "  *** Begin Patch\n*** End Patch\n "

func assertExecutorPatchOutput(t *testing.T, payload []byte) {
	t.Helper()
	item := ojson.GetBytes(payload, `output.#(type=="custom_tool_call")`)
	if item.Get("type").String() != "custom_tool_call" || item.Get("input").String() != executorPatchInput || item.Get("name").String() != "apply_patch" || item.Get("namespace").String() != "functions" {
		t.Fatalf("wrong patch output: %s", payload)
	}
}

func assertExecutorPatchStream(t *testing.T, chunks <-chan coreexecutor.StreamChunk, acknowledgeSnapshot ...func()) {
	t.Helper()
	acknowledged := len(acknowledgeSnapshot) == 0
	var delta strings.Builder
	var done, item, final ojson.Result
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		for _, line := range strings.Split(string(chunk.Payload), "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			ev := ojson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			switch ev.Get("type").String() {
			case "response.output_text.delta":
				if !acknowledged {
					acknowledged = true
					acknowledgeSnapshot[0]()
				}
			case "response.custom_tool_call_input.delta":
				if !acknowledged {
					t.Fatal("fabricated patch progress before upstream snapshot")
				}
				delta.WriteString(ev.Get("delta").String())
			case "response.custom_tool_call_input.done":
				done = ev
			case "response.output_item.done":
				if ev.Get("item.type").String() == "custom_tool_call" {
					item = ev.Get("item")
				}
			case "response.completed":
				final = ev.Get("response")
			}
		}
	}
	if !acknowledged || delta.String() != executorPatchInput || done.Get("input").String() != executorPatchInput || item.Get("input").String() != executorPatchInput || done.Get("item_id").String() != item.Get("id").String() || done.Get("call_id").String() != item.Get("call_id").String() {
		t.Fatalf("inconsistent stream delta=%q done=%s item=%s", delta.String(), done.Raw, item.Raw)
	}
	assertExecutorPatchOutput(t, []byte(final.Raw))
}
