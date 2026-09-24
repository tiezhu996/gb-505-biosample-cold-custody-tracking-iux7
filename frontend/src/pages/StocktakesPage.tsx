import { AuditOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { Button, Col, Form, Input, Modal, Row, Select, Statistic, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { stocktakeAPI, storageAPI } from '../api'
import { EntityTable } from '../components/common/EntityTable'
import { StocktakeStateBadge } from '../components/common/StocktakeBadge'
import { useAuth } from '../hooks/useAuth'
import { usePagination } from '../hooks/usePagination'
import { useStocktakeStore } from '../stores/stocktakeStore'
import type { Stocktake, StorageContainer } from '../types/domain'
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
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<{ containerId: number }>()
  const refresh = () => load({ page: pagination.page, pageSize: pagination.pageSize, search, state })
  useEffect(() => { void refresh() }, [pagination.page, pagination.pageSize, state])
  useEffect(() => {
    void storageAPI.list({ page: 1, pageSize: 100, status: 'available' }).then((result) => {
      setContainers(result.items.filter((item) => item.active && item.occupied > 0))
    })
  }, [])
  const create = async () => {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const created = await stocktakeAPI.create(values.containerId)
      message.success('盘点任务已创建，清单已固化')
      setCreateOpen(false)
      form.resetFields()
      navigate(`/stocktakes/${created.id}`)
    } finally { setSaving(false) }
  }
  const openCount = data.items.filter((item) => item.state === 'in_progress').length
  const differenceCount = data.items.filter((item) => item.missingItems + item.mislocatedItems > 0).length
  const columns: ColumnsType<Stocktake> = [
    { title: '盘点单号', dataIndex: 'stocktakeNo', fixed: 'left', render: (value, row) => <Button type="link" className="table-link" onClick={() => navigate(`/stocktakes/${row.id}`)}>{value}</Button> },
    { title: '任务月份', dataIndex: 'taskMonth' },
    { title: '盘点容器', render: (_, row) => <div><strong>{row.container?.code || row.containerId}</strong><small className="cell-subtitle">{row.container?.name}</small></div> },
    { title: '状态', dataIndex: 'state', render: (value) => <StocktakeStateBadge state={value} /> },
    { title: '处理进度', render: (_, row) => `${row.processedItems} / ${row.totalItems}` },
    { title: '差异', render: (_, row) => <span>缺失 {row.missingItems} · 位置不符 {row.mislocatedItems}</span> },
    { title: '发起人/时间', render: (_, row) => <div>{row.startedByName}<small className="cell-subtitle">{formatDateTime(row.startedAt)}</small></div> },
    { title: '关单时间', dataIndex: 'closedAt', render: formatDateTime },
    { title: '操作', fixed: 'right', render: (_, row) => <Button size="small" type={row.state === 'in_progress' ? 'primary' : 'default'} icon={<AuditOutlined />} onClick={() => navigate(`/stocktakes/${row.id}`)}>{row.state === 'in_progress' ? '继续盘点' : '查看结果'}</Button> },
  ]
  return (
    <div className="page-stack">
      <header className="page-header">
        <div>
          <Typography.Title level={2}>冻存盘点</Typography.Title>
          <Typography.Text type="secondary">每月对照任务表核对冷冻柜实物，固化在库样本原容器与格位，逐支标记在库、缺失或位置不符</Typography.Text>
        </div>
        {can('stocktake:prepare') && <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>发起盘点</Button>}
      </header>
      <Row gutter={16}>
        <Col xs={24} sm={8}><div className="metric"><Statistic title="盘点任务总数" value={data.total} /></div></Col>
        <Col xs={24} sm={8}><div className="metric"><Statistic title="进行中" value={openCount} /></div></Col>
        <Col xs={24} sm={8}><div className="metric"><Statistic title="存在差异的任务" value={differenceCount} /></div></Col>
      </Row>
      <div className="table-toolbar">
        <Input allowClear prefix={<SearchOutlined />} placeholder="搜索盘点单号、任务月份或发起人" value={search} onChange={(event) => setSearch(event.target.value)} onPressEnter={() => void refresh()} />
        <Select allowClear placeholder="全部状态" value={state} onChange={setState} options={[{ value: 'in_progress', label: '盘点中' }, { value: 'closed', label: '已关单' }, { value: 'cancelled', label: '已取消' }]} />
        <Button onClick={() => void refresh()}>查询</Button>
      </div>
      <EntityTable columns={columns} dataSource={data.items} loading={loading} emptyTitle="暂无盘点任务" pagination={{ current: pagination.page, pageSize: pagination.pageSize, total: data.total, onChange: pagination.update, showSizeChanger: true }} />
      <Modal title="发起冻存盘点" width={560} open={createOpen} confirmLoading={saving} onOk={() => void create()} onCancel={() => setCreateOpen(false)} okText="创建并固化清单" cancelText="取消">
        <Form form={form} layout="vertical">
          <Form.Item name="containerId" label="选择可用容器" rules={[{ required: true, message: '请选择要盘点的冻存容器' }]}>
            <Select showSearch optionFilterProp="label" placeholder="仅显示启用、可用且有在库样本的容器" options={containers.map((item) => ({
              value: item.id,
              label: `${item.code} · ${item.name}（在库 ${item.occupied}/${item.capacity}）`,
            }))} />
          </Form.Item>
          <Typography.Text type="secondary">创建后将把该容器当前在库样本的原容器与格位固化为清单；盘点期间若样本发生交接导致位置变化，关单时本次任务只能取消重盘。</Typography.Text>
        </Form>
      </Modal>
    </div>
  )
}
