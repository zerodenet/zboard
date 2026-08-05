from pathlib import Path


def replace_once(source: str, old: str, new: str, label: str) -> str:
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected one match, found {count}")
    return source.replace(old, new, 1)


account_path = Path("frontend/src/views/account/AccountPlans.vue")
account = account_path.read_text()
account = replace_once(
    account,
    """    const requestedPlan = plans.value.find(plan => plan.id === Number(route.query.plan))
    if (requestedPlan) await selectPlan(requestedPlan, false)
""",
    """    const requestedPlanID = Number(route.query.plan) || 0
    const requestedPlan = plans.value.find(plan => plan.id === requestedPlanID)
    if (requestedPlan) {
      await selectPlan(requestedPlan, false)
    } else if (requestedPlanID && !(operation.value === 'change' && requestedPlanID === currentPlanID)) {
      const directPlan = await fetchPlanCatalogItem(requestedPlanID)
      await selectPlan(directPlan, false)
    }
""",
    "direct linked plan loading",
)
account_path.write_text(account)

plans_path = Path("frontend/src/views/Plans.vue")
plans = plans_path.read_text()
plans = replace_once(
    plans,
    """for (const field of Object.keys(skuFieldMap)) {
  watch(() => skuDraft[field as keyof PlanSKU], () => skuErrors.clear(field))
}

const {
""",
    """for (const field of Object.keys(skuFieldMap)) {
  watch(() => skuDraft[field as keyof PlanSKU], () => skuErrors.clear(field))
}
watch(() => form.sku.billing_mode, () => {
  syncSKUCommerceFields(form.sku)
  createErrors.clear('sku.billing_mode')
  createErrors.clear('sku.allowed_operations')
})
watch(() => skuDraft.billing_mode, () => {
  syncSKUCommerceFields(skuDraft)
  skuErrors.clear('billing_mode')
  skuErrors.clear('allowed_operations')
})

const {
""",
    "billing mode interaction watchers",
)
plans_path.write_text(plans)
