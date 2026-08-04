from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


helper_path = Path("backend/internal/handler/protocol_endpoint_effects.go")
helper = helper_path.read_text()
helper = replace_once(
    helper,
    '\tprotocolEndpointPublishNotRequired = "not_required"\n\tprotocolEndpointPublishQueued      = "queued"\n',
    '\tprotocolEndpointPublishNotRequired = "not_required"\n\tprotocolEndpointPublishQueued      = "queued"\n\n\t// Runtime compilation is ordered by endpoint identity. SortOrder belongs to subscription delivery.\n\tprotocolEndpointRuntimeOrder = "id asc"\n',
    "runtime order constant",
)
helper_path.write_text(helper)

kernel_path = Path("backend/internal/handler/kernel_automation.go")
kernel = kernel_path.read_text()
kernel = replace_once(
    kernel,
    'h.db.Where("node_id = ? AND is_active = ?", node.ID, true).Order("sort_order asc, id asc").Find(&endpoints)',
    'h.db.Where("node_id = ? AND is_active = ?", node.ID, true).Order(protocolEndpointRuntimeOrder).Find(&endpoints)',
    "runtime endpoint order",
)
kernel_path.write_text(kernel)

client_path = Path("frontend/src/api/client.ts")
client = client_path.read_text()
client = replace_once(
    client,
    "\taffected_node_ids: number[]\n",
    "\taffected_node_ids?: number[]\n",
    "optional affected node IDs",
)
client_path.write_text(client)

test_path = Path("backend/internal/handler/protocol_endpoint_effects_test.go")
test = test_path.read_text()
addition = '''
func TestProtocolEndpointRuntimeOrderUsesStableIdentity(t *testing.T) {
\tif protocolEndpointRuntimeOrder != "id asc" {
\t\tt.Fatalf("runtime order = %q, want endpoint identity order", protocolEndpointRuntimeOrder)
\t}
}
'''
if "TestProtocolEndpointRuntimeOrderUsesStableIdentity" in test:
    raise SystemExit("runtime order test already exists")
test_path.write_text(test.rstrip() + "\n" + addition)
