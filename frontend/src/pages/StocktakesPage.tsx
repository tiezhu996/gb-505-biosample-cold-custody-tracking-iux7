import { EyeOutlined, FileSearchOutlined, PlusOutlined, SearchOutlined, StopOutlined } from '@ant-design/icons'
import {
  Button, Form, Input, Modal, Progress, Select, Space, Statistic, Typography, message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { stocktakeAPI, storageAPI } from '../api'
import { EntityTable } from '../components/common/EntityTable'
import { StocktakeBadge } from '../components/common/StocktakeBadge'
import { useAuth } from '../hooks/useAuth'
import { usePagination } from '../hooks/usePagination'
import { useStocktakeStore } from '../stores/stocktakeStore'
import type { StorageContainer, StocktakeTask } from '../types/domain'
import { formatDateTime } from '../utils/format'

export function StocktakesPage() {
  const { data, loading, load } = useStocktakeStore()
  const pagination = usePagination()
  const { can } = useAuth()
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [state, setState] = useState<string>()
  const [containers, setContainers] = useState<StorageContainer[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [cancelTarget, setCancelTarget] = useState<StocktakeTask | null>(null)
  const [saving, setSaving] = useState(false)
  const [createForm] = Form.useForm()
  const [cancelForm] = Form.useForm()
  const refresh = () => load({ page: pagination.page, pageSize: pagination.pageSize, search, state })
  useEffect(() => { void refresh() }, [pagination.page, pagination.pageSize, state])
  useEffect(() => {
    void storageAPI.list({ page: 1, pageSize: 100 }).then((result) => {
      setContainers(result.items.filter((item) => item.active && item.status === 'available'))
    })
  }, [])

  const activeTasks = useMemo(() => data.items.filter((item) => item.state === 'in_progress'), [data.items])

  const create = async () => {
    const values = await createForm.validateFields()
    setSaving(true)
    try {
      const task = await stocktakeAPI.create({ ...values, note: values.note || '' })
      message.success(`盘点任务 ${task.taskNo} 已创建，清单已固化`)
      setCreateOpen(false)
      createForm.resetFields()
      navigate(`/stocktakes/${task.id}`)
    } finally { setSaving(false) }
  }

  const cancelTask = async () => {
    if (!cancelTarget) return
    const values = await cancelForm.validateFields()
    setSaving(true)
    try {
      await stocktakeAPI.cancel(cancelTarget.id, values.reason)
      message.success('盘点任务已取消')
      setCancelTarget(null)
      cancelForm.resetFields()
      await refresh()
    } finally { setSaving(false) }
  }

  const columns: ColumnsType<StocktakeTask> = [
    { title: '盘点单号', dataIndex: 'taskNo', fixed: 'left' },
    { title: '状态', dataIndex: 'state', render: (value) => <StocktakeBadge value={value} /> },
    {
      title: '盘点容器',
      render: (_, row) => (
        <div>
          <strong>{row.container?.code || row.containerId}</strong>
          <small className="cell-subtitle">{row.container?.name} · {row.container?.location}</small>
        </div>
      ),
    },
    {
      title: '进度',
      render: (_, row) => {
        const done = row.inStockCount + row.missingCount + row.mismatchCount
        const percent = row.totalCount > 0 ? Math.round(done / row.totalCount * 100) : 0
        return <Progress percent={percent} size="small" status={row.pendingCount === 0 ? 'success' : 'active'} format={() => `${done}/${row.totalCount}`} />
      },
    },
    {
      title: '差异',
      render: (_, row) => (
        <Space size={4}>
          <Typography.Text type="success">在库 {row.inStockCount}</Typography.Text>
          <Typography.Text type="danger">缺失 {row.missingCount}</Typography.Text>
          <Typography.Text type="warning">不符 {row.mismatchCount}</Typography.Text>
        </Space>
      ),
    },
    { title: '发起人', dataIndex: 'startedByName' },
    { title: '开始时间', dataIndex: 'startedAt', render: formatDateTime },
    { title: '关单人/时间', render: (_, row) => row.closedAt ? <div>{row.closedByName}<small className="cell-subtitle">{formatDateTime(row.closedAt)}</small></div> : '-' },
    {
      title: '操作', fixed: 'right',
      render: (_, row) => (
        <Space>
          <Button size="small" icon={<EyeOutlined />} onClick={() => navigate(`/stocktakes/${row.id}`)}>
            {row.state === 'in_progress' ? '继续盘点' : '查看结果'}
          </Button>
          {row.state === 'in_progress' && can('stocktake:execute') && (
            <Button size="small" danger icon={<StopOutlined />} onClick={() => setCancelTarget(row)}>取消</Button>
          )}
        </Space>
      ),
    },
  ]

  return (
    <div className="page-stack">
      <header className="page-header">
        <div>
          <Typography.Title level={2}>冻存盘点</Typography.Title>
          <Typography.Text type="secondary">每月核对任务表与冷冻柜实物；清单固化后逐支标记，全部处理完才能关单换位</Typography.Text>
        </div>
        {can('stocktake:execute') && <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>发起盘点</Button>}
      </header>
      <Space size="large" wrap>
        <Statistic title="进行中任务" value={activeTasks.length} prefix={<FileSearchOutlined />} />
        <Statistic title="待标记条目" value={activeTasks.reduce((sum, task) => sum + task.pendingCount, 0)} />
        <Statistic title="未处理差异" value={activeTasks.reduce((sum, task) => sum + task.missingCount + task.mismatchCount, 0)} valueStyle={{ color: '#cf1322' }} />
      </Space>
      <div className="table-toolbar">
        <Input allowClear prefix={<SearchOutlined />} placeholder="搜索盘点单号或操作人" value={search} onChange={(event) => setSearch(event.target.value)} onPressEnter={() => void refresh()} />
        <Select allowClear placeholder="全部状态" style={{ width: 160 }} value={state} onChange={setState} options={[{ value: 'in_progress', label: '盘点中' }, { value: 'closed', label: '已关单' }, { value: 'cancelled', label: '已取消' }]} />
        <Button onClick={() => void refresh()}>查询</Button>
      </div>
      <EntityTable columns={columns} dataSource={data.items} loading={loading} emptyTitle="暂无盘点任务" pagination={{ current: pagination.page, pageSize: pagination.pageSize, total: data.total, onChange: pagination.update, showSizeChanger: true }} />

      <Modal title="发起冻存盘点" width={620} open={createOpen} confirmLoading={saving} onOk={() => void create()} onCancel={() => setCreateOpen(false)} okText="固化清单并开始" cancelText="取消">
        <Typography.Paragraph type="secondary">选择一个启用且可用的容器后，系统会把当前在库样本的原容器和格位固化成清单；盘点开始后发生交接的样本将在关单时被拦截，本次只能重新盘点。</Typography.Paragraph>
        <Form form={createForm} layout="vertical">
          <Form.Item name="taskNo" label="盘点单号" rules={[{ required: true, min: 3, max: 50 }]}>
            <Input placeholder="PD-202609-001" />
          </Form.Item>
          <Form.Item name="containerId" label="盘点容器" rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="仅可选择启用、可用状态的容器"
              options={containers.map((item) => ({ value: item.id, label: `${item.code} · ${item.name}（${item.location}，在库 ${item.occupied}/${item.capacity}）` }))}
            />
          </Form.Item>
          <Form.Item name="note" label="任务备注"><Input.TextArea rows={3} maxLength={900} showCount placeholder="例如：九月任务表例行核对" /></Form.Item>
        </Form>
      </Modal>

      <Modal title={`取消盘点 ${cancelTarget?.taskNo || ''}`} open={Boolean(cancelTarget)} confirmLoading={saving} onOk={() => void cancelTask()} onCancel={() => setCancelTarget(null)} okText="确认取消" cancelText="返回" okButtonProps={{ danger: true }}>
        <Typography.Paragraph type="secondary">取消后清单不再允许标记，也不会发生任何换位；该容器可重新发起盘点。</Typography.Paragraph>
        <Form form={cancelForm} layout="vertical">
          <Form.Item name="reason" label="取消原因" rules={[{ required: true, min: 3, max: 1000 }]}>
            <Input.TextArea rows={3} placeholder="请说明取消原因，至少 3 个字符" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
