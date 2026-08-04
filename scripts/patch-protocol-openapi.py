from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


path = Path("backend/api/openapi.yaml")
text = path.read_text()
text = replace_once(
    text,
    "      summary: Create a protocol endpoint and queue a full node configuration publish\n",
    "      summary: Create a protocol endpoint and apply its classified operational effect\n",
    "create summary",
)
text = replace_once(
    text,
    "      description: Persists the encrypted server configuration, then automatically queues validation, atomic activation, restart, local status and authenticated Connector-event checks for the node's complete current configuration.\n",
    "      description: Persists the endpoint, compares normalized plaintext configuration before encryption, and queues a complete node publish only when the active runtime changes.\n",
    "create description",
)
text = replace_once(
    text,
    '        "200": { description: Protocol endpoint configuration saved }\n',
    '''        "200":
          description: Protocol endpoint saved with classified change effects
          content:
            application/json:
              schema:
                allOf:
                  - $ref: "#/components/schemas/ApiResponse"
                  - properties:
                      data:
                        $ref: "#/components/schemas/ProtocolEndpointMutationResult"
''',
    "create response",
)
text = replace_once(
    text,
    "      summary: Update a protocol endpoint and queue a full node configuration publish\n",
    "      summary: Update a protocol endpoint and apply its classified operational effect\n",
    "update summary",
)
text = replace_once(
    text,
    "      description: The endpoint may switch carrier nodes. Existing credentials follow the endpoint, dedicated Shadowsocks ports are reallocated on the target node, and both previous and target node configurations are republished.\n",
    "      description: Delivery, billing and management changes avoid node publication. Runtime changes publish the current node; carrier-node changes migrate credentials and publish only the previous and target nodes.\n",
    "update description",
)
text = replace_once(
    text,
    '        "200": { description: Protocol endpoint configuration updated }\n',
    '''        "200":
          description: Protocol endpoint updated with classified change effects
          content:
            application/json:
              schema:
                allOf:
                  - $ref: "#/components/schemas/ApiResponse"
                  - properties:
                      data:
                        $ref: "#/components/schemas/ProtocolEndpointMutationResult"
''',
    "update response",
)
marker = "    ProtocolEndpointSelectionSnapshot:\n"
if text.count(marker) != 1:
    raise SystemExit(f"schema marker: expected exactly one match, found {text.count(marker)}")
schema = '''    ProtocolEndpointChangeEffect:
      type: string
      enum: [none, management, billing, delivery, runtime, credential_placement]
    ProtocolEndpointMutationResult:
      type: object
      required: [protocol_endpoint, effect, effects, publish_status]
      properties:
        protocol_endpoint:
          $ref: "#/components/schemas/ProtocolEndpoint"
        effect:
          $ref: "#/components/schemas/ProtocolEndpointChangeEffect"
        effects:
          type: array
          items:
            $ref: "#/components/schemas/ProtocolEndpointChangeEffect"
        publish_status:
          type: string
          enum: [queued, not_required]
        affected_node_ids:
          type: array
          items: { type: integer, format: int64 }
'''
text = text.replace(marker, schema + marker, 1)
path.write_text(text)
