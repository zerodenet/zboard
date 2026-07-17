<template>
  <section>
    <h2>Node Management</h2>
    <form @submit.prevent="create" v-if="app.isAdmin">
      <div class="grid-form">
        <input v-model="form.name" placeholder="name" />
        <input v-model="form.region" placeholder="region" />
        <input v-model="form.address" placeholder="address" />
        <input v-model="form.protocol" placeholder="protocol" />
        <input v-model="form.ssh_host" placeholder="ssh host" />
        <input v-model.number="form.ssh_port" type="number" placeholder="ssh port" />
        <input v-model="form.ssh_user" placeholder="ssh user" />
        <input v-model="form.ssh_password" type="password" placeholder="ssh password" />
        <input v-model="form.ssh_host_key_fingerprint" placeholder="SSH host key fingerprint (SHA256:...)" />
      </div>
      <button type="submit">Create Node</button>
    </form>
    <p class="muted" v-if="!app.isAdmin">Only admin users can create, test SSH, and publish protocol config.</p>
    <ul>
      <li v-for="item in nodes" :key="item.id">
        #{{ item.id }} {{ item.name }} - {{ item.region }} - {{ item.address }} - {{ item.protocol }} -
        online: {{ item.is_online ? 'yes' : 'no' }}
        <span v-if="item.ssh_host_key_fingerprint"> - host key pinned</span>
        <span v-if="item.traffic_secret_prefix">
          - report credential {{ item.traffic_secret_prefix }}...
          {{ item.traffic_secret_revoked_at ? '(revoked)' : '(active)' }}
        </span>
        <button v-if="app.isAdmin" class="small" type="button" @click="testNode(item.id)">SSH Test</button>
        <button v-if="app.isAdmin" class="small" type="button" @click="prepareSSH(item)">Configure SSH</button>
        <button v-if="app.isAdmin" class="small" type="button" @click="preparePublish(item)">Push Config</button>
        <button v-if="app.isAdmin" class="small" type="button" @click="rotateReportCredential(item)">
          {{ item.traffic_secret_prefix ? 'Rotate Report Credential' : 'Create Report Credential' }}
        </button>
        <button
          v-if="app.isAdmin && item.traffic_secret_prefix && !item.traffic_secret_revoked_at"
          class="small"
          type="button"
          @click="revokeReportCredential(item)"
        >Revoke Report Credential</button>
        <div class="node-tip" v-if="testResult[item.id]">
          {{ testResult[item.id] }}
        </div>
      </li>
    </ul>
    <p class="error" v-if="error">{{ error }}</p>

    <section class="credential-result" v-if="reportCredential.secret && app.isAdmin">
      <h3>Node Report Credential</h3>
      <p><strong>Copy this secret now. It will not be shown again.</strong></p>
      <p class="muted">Node #{{ reportCredential.node_id }} / prefix {{ reportCredential.secret_prefix }}...</p>
      <input :value="reportCredential.secret" readonly />
      <button class="small" type="button" @click="copyReportCredential">Copy Secret</button>
      <p class="muted" v-if="reportCredential.copyMessage">{{ reportCredential.copyMessage }}</p>
    </section>

    <section v-if="sshEdit.node_id && app.isAdmin">
      <h3>Configure Node SSH</h3>
      <div class="grid-form">
        <input v-model="sshEdit.ssh_host" placeholder="ssh host" />
        <input v-model.number="sshEdit.ssh_port" type="number" placeholder="ssh port" />
        <input v-model="sshEdit.ssh_user" placeholder="ssh user" />
        <input v-model="sshEdit.ssh_password" type="password" placeholder="new password (leave blank to keep current)" />
        <input v-model="sshEdit.ssh_host_key_fingerprint" placeholder="SSH host key fingerprint (SHA256:...)" />
      </div>
      <button type="button" @click="saveSSH">Save SSH Configuration</button>
    </section>

    <section v-if="publish.node_id && app.isAdmin">
      <h3>Publish Protocol Config</h3>
      <div class="grid-form">
        <label>
          Node ID:
          <input v-model.number="publish.node_id" type="number" />
        </label>
        <input v-model="publish.protocol" placeholder="protocol" />
        <textarea v-model="publish.config" rows="6" placeholder='{"listen": "0.0.0.0:443"}'></textarea>
        <textarea v-model="publish.client_config" rows="6" placeholder='Client JSON exposed in subscription, for example {"server":"node.example.com","port":443}'></textarea>
      </div>
      <button type="button" @click="publishConfig">Publish to Node</button>
      <p class="muted" v-if="publish.msg">{{ publish.msg }}</p>
    </section>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  createNode,
  fetchNodes,
  publishNodeProtocolConfig,
  revokeNodeReportCredential,
  rotateNodeReportCredential,
  testNodeSSH,
  updateNodeSSH
} from '../api/client'
import { useAppStore } from '../stores/app'

const nodes = ref<any[]>([])
const app = useAppStore()
const error = ref('')
const testResult = ref<Record<number, string>>({})
const reportCredential = reactive({
  node_id: 0,
  secret: '',
  secret_prefix: '',
  copyMessage: ''
})
const form = reactive({
  name: '',
  region: '',
  address: '',
  protocol: 'vmess',
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  ssh_host_key_fingerprint: ''
})
const sshEdit = reactive({
  node_id: 0,
  ssh_host: '',
  ssh_port: 22,
  ssh_user: '',
  ssh_password: '',
  ssh_host_key_fingerprint: ''
})
const publish = reactive({
  node_id: 0,
  protocol: 'vmess',
  config: '{\n  \"protocol\": \"vmess\"\n}',
  client_config: '{\n  \"server\": \"\",\n  \"port\": 443\n}',
  msg: ''
})

onMounted(async () => {
  try {
    nodes.value = await fetchNodes()
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || 'failed to load nodes'
  }
})

const create = async () => {
  error.value = ''
  try {
    await createNode({
      name: form.name,
      region: form.region,
      address: form.address,
      protocol: form.protocol,
      ssh_host: form.ssh_host,
      ssh_port: form.ssh_port,
      ssh_user: form.ssh_user,
      ssh_password: form.ssh_password,
      ssh_host_key_fingerprint: form.ssh_host_key_fingerprint
    })
    form.ssh_password = ''
    nodes.value = await fetchNodes()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'create failed'
  }
}

const prepareSSH = (node: any) => {
  sshEdit.node_id = node.id
  sshEdit.ssh_host = node.ssh_host || ''
  sshEdit.ssh_port = node.ssh_port || 22
  sshEdit.ssh_user = node.ssh_user || ''
  sshEdit.ssh_password = ''
  sshEdit.ssh_host_key_fingerprint = node.ssh_host_key_fingerprint || ''
}

const saveSSH = async () => {
  error.value = ''
  try {
    await updateNodeSSH(sshEdit.node_id, {
      ssh_host: sshEdit.ssh_host,
      ssh_port: sshEdit.ssh_port,
      ssh_user: sshEdit.ssh_user,
      ssh_password: sshEdit.ssh_password || undefined,
      ssh_host_key_fingerprint: sshEdit.ssh_host_key_fingerprint
    })
    sshEdit.ssh_password = ''
    nodes.value = await fetchNodes()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'SSH configuration update failed'
  }
}

const testNode = async (nodeId: number) => {
  error.value = ''
  testResult.value = { ...testResult.value, [nodeId]: '' }
  try {
    const result = await testNodeSSH(nodeId)
    testResult.value[nodeId] = `ok: ${result.output || 'pong'} (${result.latency_ms || 0}ms)`
    nodes.value = await fetchNodes()
  } catch (e: any) {
    const msg = e?.response?.data?.message || 'ssh test failed'
    testResult.value[nodeId] = `failed: ${msg}`
  }
}

const rotateReportCredential = async (node: any) => {
  if (node.traffic_secret_prefix && !window.confirm('Rotating this credential immediately invalidates the previous secret. Continue?')) {
    return
  }
  error.value = ''
  reportCredential.secret = ''
  reportCredential.copyMessage = ''
  try {
    const result = await rotateNodeReportCredential(node.id)
    reportCredential.node_id = result.node_id
    reportCredential.secret = result.secret
    reportCredential.secret_prefix = result.secret_prefix
    nodes.value = await fetchNodes()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'report credential rotation failed'
  }
}

const revokeReportCredential = async (node: any) => {
  if (!window.confirm('Revoke this node report credential? Traffic reports from this node will stop immediately.')) {
    return
  }
  error.value = ''
  try {
    await revokeNodeReportCredential(node.id)
    if (reportCredential.node_id === node.id) {
      reportCredential.secret = ''
    }
    nodes.value = await fetchNodes()
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'report credential revoke failed'
  }
}

const copyReportCredential = async () => {
  reportCredential.copyMessage = ''
  try {
    await navigator.clipboard.writeText(reportCredential.secret)
    reportCredential.copyMessage = 'Copied.'
  } catch {
    reportCredential.copyMessage = 'Copy failed. Select and copy the secret manually.'
  }
}

const preparePublish = (node: any) => {
  publish.node_id = node.id
  publish.protocol = node.protocol || 'vmess'
  publish.msg = ''
}

const publishConfig = async () => {
  error.value = ''
  publish.msg = ''
  try {
    const result = await publishNodeProtocolConfig(publish.node_id, publish.protocol, publish.config, publish.client_config)
    publish.msg = `published: node #${result.node.id}`
  } catch (e: any) {
    publish.msg = ''
    error.value = e?.response?.data?.message || 'publish failed'
  }
}
</script>

<style scoped>
.grid-form {
  display: grid;
  gap: 8px;
  max-width: 700px;
  margin-bottom: 12px;
}
input {
  border: 1px solid var(--line);
  background: #090f1e;
  color: var(--text);
  padding: 8px;
  border-radius: 8px;
}
button {
  margin-bottom: 12px;
}
.muted { color: var(--muted); }
.error { color: #ff6b6b; }
ul { padding-left: 20px; }
.node-tip { color: var(--muted); font-size: 12px; }
.credential-result {
  border: 1px solid #f5b942;
  border-radius: 8px;
  margin: 12px 0;
  padding: 12px;
}
.credential-result input { width: min(680px, 100%); }
textarea {
  width: 100%;
  min-height: 120px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #090f1e;
  color: var(--text);
  padding: 8px;
}
</style>
