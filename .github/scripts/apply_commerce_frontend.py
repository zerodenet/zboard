from pathlib import Path
import re


def replace_once(source: str, old: str, new: str, label: str) -> str:
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return source.replace(old, new, 1)


for filename in (
    "frontend/src/views/PublicPlans.vue",
    "frontend/src/views/account/AccountPlans.vue",
):
    path = Path(filename)
    source = path.read_text()
    source = source.replace('name="arrow-right"', 'name="chevron"')
    source = source.replace("icon: 'traffic'", "icon: 'billing'")
    if filename.endswith("AccountPlans.vue"):
        source = source.replace(
            "const currentOperation = computed(() => operationOptions.find(item => item.value === operation.value) || operationOptions[0])",
            "const currentOperation = computed(() => operationOptions.find(item => item.value === operation.value) || operationOptions[0]!)",
        )
        source = source.replace(
            "async function selectPlan(plan: PlanCatalogItem, sync = true) {\n  selectedPlan.value = plan\n  selectedPlanID.value = plan.id\n  requestedSKUID.value = Number(route.query.sku) || 0",
            "async function selectPlan(plan: PlanCatalogItem, sync = true) {\n  selectedPlan.value = plan\n  selectedPlanID.value = plan.id\n  requestedSKUID.value = sync ? 0 : Number(route.query.sku) || 0",
        )
    path.write_text(source)


path = Path("frontend/src/views/Plans.vue")
source = path.read_text()

source = replace_once(
    source,
    "<span>{{ sku.code }} · {{ skuTypeLabel(sku.sku_type) }}</span>",
    "<span>{{ sku.code }}</span>",
    "SKU row subtitle",
)
source = replace_once(
    source,
    '<th data-column-priority="2">权益来源</th>',
    '<th data-column-priority="2">可用场景</th>\n                <th data-column-priority="3">权益来源</th>',
    "SKU operation header",
)
source = replace_once(
    source,
    "<td data-column-priority=\"2\">{{ sku.sku_type === 'traffic_pack' ? `增加 ${formatBytes(sku.grant_traffic_bytes)}` : '继承商品权益' }}</td>",
    "<td data-column-priority=\"2\"><span class=\"sku-operation-summary\">{{ operationSummary(sku.allowed_operations) }}</span></td>\n                <td data-column-priority=\"3\">{{ sku.billing_mode === 'one_time' ? `增加 ${formatBytes(sku.grant_traffic_bytes)}` : '继承商品权益' }}</td>",
    "SKU operation cells",
)

create_type_field = "<FormField v-slot=\"{ controlAttrs }\" label=\"规格类型\" name=\"create-plan-sku-type\" :error=\"createErrors.fields['sku.sku_type']\"><UiSelect v-model=\"form.sku.sku_type\" v-bind=\"controlAttrs\" :options=\"skuTypeOptions\" /></FormField>"
create_commerce_fields = """<FormField v-slot="{ controlAttrs }" label="计费方式" name="create-plan-billing-mode" :error="createErrors.fields['sku.billing_mode']"><UiSelect v-model="form.sku.billing_mode" v-bind="controlAttrs" :options="billingModeOptions" /></FormField>
            <FormField label="可用场景" name="create-plan-operations" :error="createErrors.fields['sku.allowed_operations']" full>
              <div class="sku-operation-grid">
                <label v-for="option in skuOperationOptions" :key="option.value" class="sku-operation-option">
                  <UiCheckbox :model-value="skuOperationsFor(form.sku).includes(option.value)" @update:model-value="toggleSKUOperation(form.sku, option.value, $event)" />
                  <span><strong>{{ option.label }}</strong><small>{{ option.description }}</small></span>
                </label>
              </div>
            </FormField>"""
source = replace_once(source, create_type_field, create_commerce_fields, "create commerce fields")

edit_type_field = "<FormField v-slot=\"{ controlAttrs }\" label=\"规格类型\" name=\"edit-sku-type\" :error=\"skuErrors.fields.sku_type\"><UiSelect v-model=\"skuDraft.sku_type\" v-bind=\"controlAttrs\" :options=\"skuTypeOptions\" /></FormField>"
edit_commerce_fields = """<FormField v-slot="{ controlAttrs }" label="计费方式" name="edit-sku-billing-mode" :error="skuErrors.fields.billing_mode"><UiSelect v-model="skuDraft.billing_mode" v-bind="controlAttrs" :options="billingModeOptions" /></FormField>
        <FormField label="可用场景" name="edit-sku-operations" :error="skuErrors.fields.allowed_operations" full>
          <div class="sku-operation-grid">
            <label v-for="option in skuOperationOptions" :key="option.value" class="sku-operation-option">
              <UiCheckbox :model-value="skuOperationsFor(skuDraft).includes(option.value)" @update:model-value="toggleSKUOperation(skuDraft, option.value, $event)" />
              <span><strong>{{ option.label }}</strong><small>{{ option.description }}</small></span>
            </label>
          </div>
        </FormField>"""
source = replace_once(source, edit_type_field, edit_commerce_fields, "edit commerce fields")
source = source.replace(
    'v-if="skuDraft.sku_type === \'traffic_pack\'"',
    'v-if="skuDraft.billing_mode === \'one_time\'"',
)

source = replace_once(
    source,
    "  sku_type: 'new',\n  billing_unit: 'month',",
    "  sku_type: 'new',\n  billing_mode: 'periodic' as const,\n  allowed_operations: ['purchase', 'renew'] as Array<'purchase' | 'renew' | 'change' | 'addon'>,\n  billing_unit: 'month',",
    "empty SKU commerce defaults",
)

source = source.replace(
    "  'skus.0.sku_type': 'sku.sku_type',",
    "  'skus.0.billing_mode': 'sku.billing_mode',\n  'skus.0.allowed_operations': 'sku.allowed_operations',",
)
source = source.replace(
    "  sku_type: 'sku_type',",
    "  billing_mode: 'billing_mode',\n  allowed_operations: 'allowed_operations',",
)

options_pattern = re.compile(r"const skuTypeOptions = \[.*?\]\nconst billingUnitOptions", re.S)
options_replacement = """const billingModeOptions = [
  { label: '周期计费', value: 'periodic' },
  { label: '一次性计费', value: 'one_time' },
]
const skuOperationOptions = [
  { label: '新购', value: 'purchase' as const, description: '允许用户创建新的独立订阅。' },
  { label: '续费', value: 'renew' as const, description: '允许为同一商品的现有订阅延长周期。' },
  { label: '套餐切换', value: 'change' as const, description: '允许其他商品的订阅切换到当前商品。' },
  { label: '附加购买', value: 'addon' as const, description: '一次性增加目标订阅的附加权益。' },
]
const billingUnitOptions"""
source, count = options_pattern.subn(options_replacement, source, count=1)
if count != 1:
    raise SystemExit("SKU options block not found")

source = source.replace(
    "const skuTypes = ['new', 'renewal', 'upgrade', 'traffic_pack'] as const",
    "const billingModes = ['periodic', 'one_time'] as const\nconst skuOperations = ['purchase', 'renew', 'change', 'addon'] as const",
)
source = source.replace(
    "  [() => form.sku.sku_type, 'sku.sku_type'],",
    "  [() => form.sku.billing_mode, 'sku.billing_mode'],\n  [() => form.sku.allowed_operations, 'sku.allowed_operations'],",
)

label_pattern = re.compile(
    r"function skuTypeLabel\(type: string\) \{.*?\n\}\n\nfunction billingLabel",
    re.S,
)
label_replacement = """function skuOperationsFor(sku: Pick<PlanSKU, 'sku_type' | 'allowed_operations'>) {
  if (sku.allowed_operations?.length) return sku.allowed_operations
  const legacyOperation = ({ new: 'purchase', renewal: 'renew', upgrade: 'change', traffic_pack: 'addon' } as const)[sku.sku_type as 'new' | 'renewal' | 'upgrade' | 'traffic_pack']
  return [legacyOperation || 'purchase'] as Array<'purchase' | 'renew' | 'change' | 'addon'>
}

function operationSummary(operations?: PlanSKU['allowed_operations']) {
  const values = operations?.length ? operations : ['purchase']
  const labels: Record<string, string> = { purchase: '新购', renew: '续费', change: '套餐切换', addon: '附加购买' }
  return values.map(value => labels[value] || value).join('、')
}

function syncSKUCommerceFields(sku: PlanSKU | ReturnType<typeof emptySKU>) {
  if (sku.billing_mode === 'one_time') {
    sku.billing_unit = 'once'
    sku.allowed_operations = ['addon']
    sku.sku_type = 'traffic_pack'
    return
  }
  sku.billing_mode = 'periodic'
  if (sku.billing_unit === 'once') sku.billing_unit = 'month'
  const operations = skuOperationsFor(sku).filter(operation => operation !== 'addon')
  sku.allowed_operations = operations.length ? operations : ['purchase']
  sku.sku_type = sku.allowed_operations.includes('purchase')
    ? 'new'
    : sku.allowed_operations.includes('renew')
      ? 'renewal'
      : 'upgrade'
  sku.grant_traffic_bytes = 0
}

function toggleSKUOperation(
  sku: PlanSKU | ReturnType<typeof emptySKU>,
  operation: 'purchase' | 'renew' | 'change' | 'addon',
  enabled: boolean,
) {
  const operations = new Set(skuOperationsFor(sku))
  if (enabled) operations.add(operation)
  else operations.delete(operation)
  sku.allowed_operations = Array.from(operations)
  if (operation === 'addon' && enabled) sku.billing_mode = 'one_time'
  syncSKUCommerceFields(sku)
}

function billingLabel"""
source, count = label_pattern.subn(label_replacement, source, count=1)
if count != 1:
    raise SystemExit("SKU label function not found")

validation_pattern = re.compile(
    r"function skuValidation\(sku: PlanSKU \| ReturnType<typeof emptySKU>, prefix = ''\) \{.*?\n\}\n\nasync function openDetails",
    re.S,
)
validation_replacement = """function skuValidation(sku: PlanSKU | ReturnType<typeof emptySKU>, prefix = '') {
  const field = (name: string) => `${prefix}${name}`
  const billingMode = sku.billing_mode || (sku.billing_unit === 'once' ? 'one_time' : 'periodic')
  const operations = skuOperationsFor(sku)
  const billingUnitValid = isOneOf(sku.billing_unit, billingUnits)
  const operationsValid = operations.length > 0 && operations.every(operation => isOneOf(operation, skuOperations))
  return collectFieldErrors({
    [field('name')]: !isUtf8LengthInRange(sku.name, 1, 80, true) && '规格名称需包含 1–80 个 UTF-8 字节。',
    [field('code')]: !isSlug(sku.code, 80) && 'SKU 编码只能包含小写字母、数字和单个连字符。',
    [field('currency')]: !isUtf8LengthInRange(sku.currency, 1, 8, true) && '币种需包含 1–8 个 UTF-8 字节。',
    [field('billing_mode')]: !isOneOf(billingMode, billingModes) && '请选择有效的计费方式。',
    [field('allowed_operations')]: !operationsValid
      ? '请至少选择一个有效的可用场景。'
      : billingMode === 'one_time' && (operations.length !== 1 || operations[0] !== 'addon')
        ? '一次性计费当前仅支持附加购买。'
        : billingMode === 'periodic' && operations.includes('addon')
          ? '附加购买必须使用一次性计费。'
          : false,
    [field('billing_unit')]: !billingUnitValid
      ? '请选择有效的计费单位。'
      : billingMode === 'one_time' && sku.billing_unit !== 'once'
        ? '一次性计费必须使用一次性单位。'
        : billingMode === 'periodic' && sku.billing_unit === 'once'
          ? '周期计费不能使用一次性单位。'
          : false,
    [field('billing_value')]: !isIntegerInRange(sku.billing_value, 1, Number.MAX_SAFE_INTEGER) && '周期数量必须为大于 0 的整数。',
    [field('price_cents')]: !isIntegerInRange(sku.price_cents, 0, Number.MAX_SAFE_INTEGER) && '价格必须为不小于 0 的整数分。',
    [field('grant_traffic_bytes')]: billingMode === 'one_time' && !isIntegerInRange(sku.grant_traffic_bytes, 1, Number.MAX_SAFE_INTEGER) && '附加购买的流量必须大于 0。',
    [field('sort_order')]: !isIntegerInRange(sku.sort_order, Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER) && '排序必须为整数。',
  })
}

async function openDetails"""
source, count = validation_pattern.subn(validation_replacement, source, count=1)
if count != 1:
    raise SystemExit("SKU validation block not found")

source = source.replace(
    "        sku_type: form.sku.sku_type,\n        billing_unit: form.sku.billing_unit,",
    "        billing_mode: form.sku.billing_mode,\n        allowed_operations: skuOperationsFor(form.sku),\n        billing_unit: form.sku.billing_unit,",
)
source = source.replace(
    "        grant_traffic_bytes: form.sku.sku_type === 'traffic_pack' ? form.sku.grant_traffic_bytes : 0,",
    "        grant_traffic_bytes: form.sku.billing_mode === 'one_time' ? form.sku.grant_traffic_bytes : 0,",
)
source = source.replace(
    "    sku_type: skuDraft.sku_type,\n    billing_unit: skuDraft.billing_unit,",
    "    billing_mode: skuDraft.billing_mode,\n    allowed_operations: skuOperationsFor(skuDraft),\n    billing_unit: skuDraft.billing_unit,",
)
source = source.replace(
    "    grant_traffic_bytes: skuDraft.sku_type === 'traffic_pack' ? skuDraft.grant_traffic_bytes : 0,",
    "    grant_traffic_bytes: skuDraft.billing_mode === 'one_time' ? skuDraft.grant_traffic_bytes : 0,",
)

source = source.replace(
    "      Object.assign(skuDraft, JSON.parse(JSON.stringify(source)))\n      skuErrors.clear()",
    "      Object.assign(skuDraft, JSON.parse(JSON.stringify(source)))\n      skuDraft.allowed_operations = skuOperationsFor(skuDraft)\n      skuDraft.billing_mode = skuDraft.billing_mode || (skuDraft.billing_unit === 'once' ? 'one_time' : 'periodic')\n      syncSKUCommerceFields(skuDraft)\n      skuErrors.clear()",
)
source = source.replace(
    "  normalizeSKUInput(form.sku)\n  const firstSKUValidation",
    "  normalizeSKUInput(form.sku)\n  syncSKUCommerceFields(form.sku)\n  const firstSKUValidation",
)
source = source.replace(
    "  normalizeSKUInput(skuDraft)\n  const valid",
    "  normalizeSKUInput(skuDraft)\n  syncSKUCommerceFields(skuDraft)\n  const valid",
)

path.write_text(source)
