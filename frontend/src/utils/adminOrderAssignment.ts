import type { PlanSKU } from '../api/client'
export interface AssignmentChoice {
  id: number
  label: string
  planId?: number
  sku?: PlanSKU
}
