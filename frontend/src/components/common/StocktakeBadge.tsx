import { Tag } from 'antd'
import type { StocktakeResult, StocktakeState } from '../../types/domain'

const stateLabels: Record<StocktakeState, { label: string; color: string }> = {
  in_progress: { label: '盘点中', color: 'processing' },
  closed: { label: '已关单', color: 'success' },
  cancelled: { label: '已取消（需重盘）', color: 'default' },
}

const resultLabels: Record<StocktakeResult, { label: string; color: string }> = {
  pending: { label: '待核对', color: 'default' },
  in_place: { label: '在库', color: 'success' },
  missing: { label: '缺失', color: 'error' },
  mislocated: { label: '位置不符', color: 'warning' },
}

export function StocktakeStateBadge({ state }: { state: StocktakeState }) {
  const item = stateLabels[state] || { label: state, color: 'default' }
  return <Tag color={item.color}>{item.label}</Tag>
}

export function StocktakeResultBadge({ result }: { result: StocktakeResult }) {
  const item = resultLabels[result] || { label: result, color: 'default' }
  return <Tag color={item.color}>{item.label}</Tag>
}
