import { Tag } from 'antd'
import type { StocktakeResult, StocktakeState } from '../../types/domain'

const taskStates: Record<StocktakeState, { label: string; color: string }> = {
  in_progress: { label: '盘点中', color: 'processing' },
  closed: { label: '已关单', color: 'success' },
  cancelled: { label: '已取消', color: 'default' },
}

const itemResults: Record<StocktakeResult, { label: string; color: string }> = {
  in_stock: { label: '在库', color: 'success' },
  missing: { label: '缺失', color: 'error' },
  mismatched: { label: '位置不符', color: 'warning' },
}

export function StocktakeBadge({ value }: { value: StocktakeState | StocktakeResult | '' }) {
  if (value === '') return <Tag>待核对</Tag>
  const item = taskStates[value as StocktakeState] || itemResults[value as StocktakeResult]
  return <Tag color={item?.color || 'default'}>{item?.label || value}</Tag>
}
