import { ArrowLeftOutlined, CheckCircleOutlined, EnvironmentOutlined, ExclamationCircleOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Col, Descriptions, Form, Input, Modal, Progress, Radio, Row, Select, Space, Statistic, Tooltip, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { stocktakeAPI, storageAPI } from '../api'
import { EntityTable } from '../components/common/EntityTable'
import { StocktakeResultBadge, StocktakeStateBadge } from '../components/common/StocktakeBadge'
import { useAuth } from '../hooks/useAuth'
import type { Stocktake, StocktakeItem, StocktakeResult, StorageContainer } from '../types/domain'
import { formatDateTime } from '../utils/format'

type MarkValue = Exclude<StocktakeResult, 'pending'>

export function StocktakeDetailPage() {
  const { id } = useParams()
  const stocktakeId = Number(id)
  const navigate = useNavigate()
  const { can } = useAuth()
  const [task, setTask] = useState<Stocktake | null>(null)
  const [loading, setLoading] = useState(false)
  const [containers, setContainers] = useState<StorageContainer[]>([])
  const [markTarget, setMarkTarget] = useState<StocktakeItem | null>(null)
  const [markValue, setMarkValue] = useState<MarkValue>('in_place')
  const [saving, setSaving] = useState(false)
  const [cancelOpen, setCancelOpen] = useState(false)
  const [form] = Form.useForm()
  const [cancelForm] = Form.useForm<{ reason: string }>()
  const open = task?.state === 'in_progress'
  const load = useCallback(async () => {
    setLoading(true)
    try { setTask(await stocktakeAPI.get(stocktakeId)) } finally { setLoading(false) }
  }, [stocktakeId])
  useEffect(() => { void load() }, [load])
  useEffect(() => {
    void storageAPI.list({ page: 1, pageSize: 100 }).then((result) => {
      setContainers(result.items.filter((item) => item.active && item.status === 'available'))
    })
  }, [])
  const openMark = (item: StocktakeItem) => {
    setMarkTarget(item)
    const initial: MarkValue = item.result === 'pending' ? 'in_place' : item.result as MarkValue
    setMarkValue(initial)
    form.setFieldsValue({
      result: initial,
      note: item.note || '',
      newContainerId: item.newContainerId,
      newPosition: item.newPosition || '',
    })
  }
  const submitMark = async () => {
    if (!markTarget) return
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = {
        result: markValue,
        note: values.note || '',
        newContainerId: markValue === 'mislocated' ? values.newContainerId : undefined,
        newPosition: markValue === 'mislocated' ? values.newPosition : undefined,
      }
      const updated = await stocktakeAPI.markItem(stocktakeId, markTarget.id, payload)
      message.success('条目标记已保存')
      setMarkTarget(null)
      form.resetFields()
      setTask(updated)
    } finally { setSaving(false) }
  }
  const close = () => {
    Modal.confirm({
      title: '确认关单并一次性换位？',
      icon: <CheckCircleOutlined />,
      content: '关单后将按标记结果统一调整：缺失样本移出容器并置为已处置，位置不符样本迁入新容器新格位，同时调整两边容器占用。操作不可撤销。',
      okText: '确认关单',
      cancelText: '再核对一下',
      onOk: async () => {
        try {
          const closed = await stocktakeAPI.close(stocktakeId)
          message.success('盘点已关单，位置与容器占用已同步调整')
          setTask(closed)
        } catch {
          await load()
        }
      },
    })
  }
  const submitCancel = async () => {
    const values = await cancelForm.validateFields()
    setSaving(true)
    try {
      const cancelled = await stocktakeAPI.cancel(stocktakeId, values.reason)
      message.success('盘点任务已取消，需重新发起盘点')
      setCancelOpen(false)
      cancelForm.resetFields()
      setTask(cancelled)
    } finally { setSaving(false) }
  }
  const percent = task && task.totalItems ? Math.round(task.processedItems / task.totalItems * 100) : 0
  const columns: ColumnsType<StocktakeItem> = [
    { title: '样本接收号', render: (_, row) => <div><strong>{row.specimen?.accessionNo || row.specimenId}</strong><small className="cell-subtitle">{row.specimen?.sampleType}</small></div> },
    { title: '原容器（清单固化）', render: (_, row) => `${row.originContainerCode} · ${row.originContainerName}` },
    { title: '原格位', dataIndex: 'originPosition' },
    { title: '核对结果', dataIndex: 'result', render: (value) => <StocktakeResultBadge result={value} /> },
    { title: '新容器 / 新格位', render: (_, row) => row.result === 'mislocated' ? `${row.newContainerCode || '-'} / ${row.newPosition || '-'}` : '-' },
    { title: '说明', dataIndex: 'note', ellipsis: true, render: (value) => value || '-' },
    { title: '标记人/时间', render: (_, row) => row.markedAt ? <div>{row.markedByName}<small className="cell-subtitle">{formatDateTime(row.markedAt)}</small></div> : '-' },
    ...(open && can('stocktake:prepare') ? [{
      title: '操作', fixed: 'right' as const,
      render: (_: unknown, row: StocktakeItem) => <Button size="small" type={row.result === 'pending' ? 'primary' : 'default'} onClick={() => openMark(row)}>{row.result === 'pending' ? '标记' : '改判'}</Button>,
    }] : []),
  ]
  return (
    <div className="page-stack">
      <header className="page-header">
        <div>
          <Button type="link" icon={<ArrowLeftOutlined />} onClick={() => navigate('/stocktakes')} style={{ paddingLeft: 0 }}>返回盘点任务</Button>
          <Typography.Title level={2} style={{ marginTop: 0 }}>盘点任务 {task?.stocktakeNo || ''}</Typography.Title>
          <Typography.Text type="secondary">任务月份 {task?.taskMonth} · 容器 {task?.container ? `${task.container.code}（${task.container.name}）` : ''}</Typography.Text>
        </div>
        {task && <StocktakeStateBadge state={task.state} />}
      </header>

      {task?.state === 'cancelled' && (
        <Alert type="warning" showIcon icon={<ExclamationCircleOutlined />} message="本次盘点已取消，清单未生效" description={task.cancelReason ? `取消原因：${task.cancelReason}。样本已发生交接或位置变化，请重新发起盘点。` : '样本已发生交接或位置变化，请重新发起盘点。'} />
      )}
      {task?.state === 'closed' && (task.missingItems > 0 || task.mislocatedItems > 0) && (
        <Alert type="info" showIcon icon={<EnvironmentOutlined />} message="盘点差异已处理" description={`缺失 ${task.missingItems} 支已移出容器并登记处置，位置不符 ${task.mislocatedItems} 支已迁入新格位，两边容器占用已同步调整。`} />
      )}

      <Row gutter={16}>
        <Col xs={24} sm={12} lg={6}><div className="metric"><Statistic title="清单总数" value={task?.totalItems || 0} /></div></Col>
        <Col xs={24} sm={12} lg={6}><div className="metric"><Statistic title="在库" value={task?.inPlaceItems || 0} valueStyle={{ color: '#3f8600' }} /></div></Col>
        <Col xs={24} sm={12} lg={6}><div className="metric"><Statistic title="缺失" value={task?.missingItems || 0} valueStyle={{ color: '#cf1322' }} /></div></Col>
        <Col xs={24} sm={12} lg={6}><div className="metric"><Statistic title="位置不符" value={task?.mislocatedItems || 0} valueStyle={{ color: '#d48806' }} /></div></Col>
      </Row>

      <Card>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Space style={{ marginBottom: 8 }}><Typography.Text strong>处理进度</Typography.Text><Typography.Text type="secondary">{task?.processedItems || 0} / {task?.totalItems || 0}</Typography.Text></Space>
            <Progress percent={percent} status={percent === 100 ? 'success' : 'active'} />
          </div>
          <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }}>
            <Descriptions.Item label="发起人">{task?.startedByName} · {formatDateTime(task?.startedAt)}</Descriptions.Item>
            <Descriptions.Item label="关单人">{task?.closedByName ? `${task.closedByName} · ${formatDateTime(task.closedAt)}` : '-'}</Descriptions.Item>
            <Descriptions.Item label="关单状态">{open ? `未关单，剩余 ${(task?.totalItems || 0) - (task?.processedItems || 0)} 条待处理` : '已结束'}</Descriptions.Item>
          </Descriptions>
          {open && (
            <Space wrap>
              <Tooltip title={task?.processedItems !== task?.totalItems ? '全部条目处理完才能关单' : ''}>
                <Button type="primary" icon={<CheckCircleOutlined />} disabled={task?.processedItems !== task?.totalItems} onClick={close}>
                  {task?.processedItems === task?.totalItems ? '关单并一次性换位' : `还差 ${(task?.totalItems || 0) - (task?.processedItems || 0)} 条未处理`}
                </Button>
              </Tooltip>
              {can('stocktake:prepare') && <Button danger icon={<StopOutlined />} onClick={() => setCancelOpen(true)}>取消并重盘</Button>}
            </Space>
          )}
        </Space>
      </Card>

      <EntityTable columns={columns} dataSource={task?.items || []} loading={loading} rowKey="id" pagination={false} emptyTitle="清单为空或加载中" />

      <Modal title={`标记核对结果 · ${markTarget?.specimen?.accessionNo || ''}`} width={600} open={Boolean(markTarget)} confirmLoading={saving} onOk={() => void submitMark()} onCancel={() => { setMarkTarget(null); form.resetFields() }} okText="保存标记" cancelText="取消">
        {markTarget && (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Descriptions size="small" column={1} bordered>
              <Descriptions.Item label="清单固化位置">{markTarget.originContainerCode} / {markTarget.originPosition}</Descriptions.Item>
            </Descriptions>
            <Form form={form} layout="vertical" initialValues={{ result: 'in_place' }} onValuesChange={(changed) => {
              if ('result' in changed) setMarkValue(changed.result as MarkValue)
            }}>
              <Form.Item name="result" label="实物核对结果" rules={[{ required: true }]}>
                <Radio.Group optionType="button" buttonStyle="solid" options={[
                  { value: 'in_place', label: '在库（位置一致）' },
                  { value: 'missing', label: '缺失' },
                  { value: 'mislocated', label: '位置不符' },
                ]} />
              </Form.Item>
              {markValue === 'mislocated' && (
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item name="newContainerId" label="新容器" rules={[{ required: true, message: '请选择新容器' }]}>
                      <Select showSearch optionFilterProp="label" placeholder="选择实际所在容器" options={containers.map((item) => ({ value: item.id, label: `${item.code} · ${item.name}` }))} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="newPosition" label="新格位" rules={[{ required: true, message: '请填写新格位' }]}>
                      <Input placeholder="实际核对到的格位，如 R03-B05-C07" />
                    </Form.Item>
                  </Col>
                </Row>
              )}
              <Form.Item name="note" label={markValue === 'missing' ? '缺失说明（必填）' : '备注说明'} rules={markValue === 'missing' ? [{ required: true, min: 2, message: '标记缺失必须填写说明' }] : []}>
                <Input.TextArea rows={3} maxLength={900} showCount placeholder={markValue === 'missing' ? '描述查找经过、可能原因等' : '可选'} />
              </Form.Item>
            </Form>
            <Alert type="warning" showIcon message="若盘点开始后该样本已交接、现位置与清单不一致，关单时系统会拒绝覆盖，本任务只能取消后重新盘点。" />
          </Space>
        )}
      </Modal>

      <Modal title="取消盘点任务" open={cancelOpen} confirmLoading={saving} onOk={() => void submitCancel()} onCancel={() => setCancelOpen(false)} okText="确认取消并重盘" cancelText="返回" okButtonProps={{ danger: true }}>
        <Form form={cancelForm} layout="vertical">
          <Alert type="warning" showIcon style={{ marginBottom: 16 }} message="取消后清单不生效、样本位置不变；如样本已交接导致位置漂移，必须重新发起盘点。" />
          <Form.Item name="reason" label="取消原因" rules={[{ required: true, min: 3, message: '请填写至少 3 个字符的原因' }]}>
            <Input.TextArea rows={3} maxLength={900} showCount placeholder="说明取消或需要重盘的原因" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
