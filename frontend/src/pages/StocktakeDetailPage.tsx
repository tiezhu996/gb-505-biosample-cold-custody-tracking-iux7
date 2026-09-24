import {
  ArrowLeftOutlined, CheckCircleOutlined, ExclamationCircleOutlined, LockOutlined,
  SearchOutlined, SwapOutlined,
} from '@ant-design/icons'
import {
  Alert, Button, Card, Col, Descriptions, Form, Input, Modal, Progress, Row, Select,
  Space, Statistic, Table, Tag, Typography, message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { stocktakeAPI, storageAPI } from '../api'
import { StocktakeBadge } from '../components/common/StocktakeBadge'
import { useAuth } from '../hooks/useAuth'
import type { StorageContainer, StocktakeItem, StocktakeResult, StocktakeTask } from '../types/domain'
import { formatDateTime } from '../utils/format'

type ResultFilter = StocktakeResult | 'pending' | ''

export function StocktakeDetailPage() {
  const { id } = useParams()
  const taskId = Number(id)
  const navigate = useNavigate()
  const { can } = useAuth()
  const [task, setTask] = useState<StocktakeTask | null>(null)
  const [items, setItems] = useState<StocktakeItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [search, setSearch] = useState('')
  const [resultFilter, setResultFilter] = useState<ResultFilter>('')
  const [containers, setContainers] = useState<StorageContainer[]>([])
  const [markTarget, setMarkTarget] = useState<StocktakeItem | null>(null)
  const [markResult, setMarkResult] = useState<StocktakeResult>('in_stock')
  const [saving, setSaving] = useState(false)
  const [markForm] = Form.useForm()

  const loadItems = useCallback(async () => {
    setLoading(true)
    try {
      const result = await stocktakeAPI.listItems(taskId, {
        page, pageSize, search,
        pending: resultFilter === 'pending',
        result: resultFilter && resultFilter !== 'pending' ? resultFilter : undefined,
      })
      setItems(result.items)
      setTotal(result.total)
    } finally { setLoading(false) }
  }, [taskId, page, pageSize, search, resultFilter])

  const reload = useCallback(async () => {
    setTask(await stocktakeAPI.get(taskId))
    await loadItems()
  }, [taskId, loadItems])

  useEffect(() => { void stocktakeAPI.get(taskId).then(setTask) }, [taskId])
  useEffect(() => { void loadItems() }, [loadItems])
  useEffect(() => {
    void storageAPI.list({ page: 1, pageSize: 100 }).then((result) => {
      setContainers(result.items.filter((item) => item.active && item.status === 'available'))
    })
  }, [])

  const inProgress = task?.state === 'in_progress'
  const doneCount = (task?.inStockCount ?? 0) + (task?.missingCount ?? 0) + (task?.mismatchCount ?? 0)
  const percent = task && task.totalCount > 0 ? Math.round(doneCount / task.totalCount * 100) : 0
  const driftHint = useMemo(() => Boolean(task && task.state === 'in_progress' && task.pendingCount === 0 && task.mismatchCount > 0), [task])

  const openMark = (item: StocktakeItem, preset?: StocktakeResult) => {
    setMarkTarget(item)
    const initial = preset || (item.result === '' ? 'in_stock' : item.result)
    setMarkResult(initial)
    markForm.setFieldsValue({
      result: initial,
      remark: item.remark || '',
      newContainerId: item.newContainerId,
      newPosition: item.newPosition || '',
    })
  }

  const submitMark = async () => {
    if (!markTarget) return
    const values = await markForm.validateFields()
    setSaving(true)
    try {
      await stocktakeAPI.markItem(markTarget.id, {
        result: values.result,
        remark: values.remark || '',
        newContainerId: values.result === 'mismatched' ? values.newContainerId : undefined,
        newPosition: values.result === 'mismatched' ? values.newPosition : undefined,
      })
      message.success('盘点结果已保存')
      setMarkTarget(null)
      markForm.resetFields()
      await reload()
    } finally { setSaving(false) }
  }

  const closeTask = () => {
    if (!task) return
    Modal.confirm({
      title: `确认关单 ${task.taskNo}？`,
      icon: <LockOutlined />,
      content: task.mismatchCount > 0
        ? `关单后将一次性把 ${task.mismatchCount} 支位置不符样本换到新格位，并同步调整两边容器占用；在库与缺失样本登记位置不变。若盘点期间有样本已交接，关单会被拦截，本次任务只能取消后重盘。`
        : '关单后盘点结果固化，不能再修改条目。',
      okText: '确认关单',
      cancelText: '返回',
      onOk: async () => {
        try {
          await stocktakeAPI.close(task.id)
          message.success('盘点已关单，位置调整完成')
          await reload()
        } catch { /* 全局拦截器已提示 */ }
      },
    })
  }

  const columns: ColumnsType<StocktakeItem> = [
    {
      title: '样本接收号', dataIndex: 'accessionNo', fixed: 'left',
      render: (value, row) => <div><strong>{value}</strong><small className="cell-subtitle">{row.sampleType}</small></div>,
    },
    { title: '原容器', render: (_, row) => row.originContainer?.code || row.originContainerId },
    { title: '原格位', dataIndex: 'originPosition' },
    {
      title: '当前登记位置',
      render: (_, row) => {
        const current = row.specimen?.storageContainer
          ? `${row.specimen.storageContainer.code} / ${row.specimen.position || '-'}`
          : '不在库'
        const moved = row.specimen && (row.specimen.storageContainerId !== row.originContainerId || row.specimen.position !== row.originPosition)
        return <Space size={4}>{current}{moved && inProgress && <Tag color="error" icon={<ExclamationCircleOutlined />}>已变动</Tag>}</Space>
      },
    },
    { title: '核对结果', dataIndex: 'result', render: (value) => <StocktakeBadge value={value} /> },
    {
      title: '新位置（关单生效）',
      render: (_, row) => row.newContainer
        ? <div>{row.newContainer.code}<small className="cell-subtitle">{row.newPosition}</small></div>
        : '-',
    },
    { title: '说明', dataIndex: 'remark', ellipsis: true, render: (value) => value || '-' },
    { title: '核对人', dataIndex: 'checkedByName', render: (value, row) => value ? <div>{value}<small className="cell-subtitle">{formatDateTime(row.checkedAt)}</small></div> : '-' },
    {
      title: '操作', fixed: 'right',
      render: (_, row) => inProgress && can('stocktake:execute') ? (
        <Space direction="vertical" size={4}>
          <Space size={4}>
            <Button size="small" type={row.result === 'in_stock' ? 'primary' : 'default'} ghost={row.result === 'in_stock'} icon={<CheckCircleOutlined />} onClick={() => openMark(row, 'in_stock')}>在库</Button>
            <Button size="small" danger={row.result === 'missing'} onClick={() => openMark(row, 'missing')}>缺失</Button>
            <Button size="small" type="primary" ghost danger={row.result === 'mismatched'} icon={<SwapOutlined />} onClick={() => openMark(row, 'mismatched')}>位置不符</Button>
          </Space>
        </Space>
      ) : '-',
    },
  ]

  return (
    <div className="page-stack">
      <Space>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/stocktakes')}>返回盘点列表</Button>
      </Space>
      <header className="page-header">
        <div>
          <Typography.Title level={2}>{task?.taskNo} <StocktakeBadge value={task?.state || 'in_progress'} /></Typography.Title>
          <Typography.Text type="secondary">
            {task?.container?.code} · {task?.container?.name}（{task?.container?.location}）清单固化于 {formatDateTime(task?.startedAt)}
          </Typography.Text>
        </div>
        {inProgress && can('stocktake:execute') && (
          <Button type="primary" icon={<LockOutlined />} disabled={!task || task.pendingCount > 0} onClick={closeTask}>
            {task && task.pendingCount > 0 ? `还有 ${task.pendingCount} 支未标记` : '关单并一次性换位'}
          </Button>
        )}
      </header>

      {task?.state === 'cancelled' && (
        <Alert type="warning" showIcon message="该盘点任务已取消，未发生任何换位" description={`取消原因：${task.cancelReason || '-'}；关单操作人：${task.closedByName || '-'}，时间：${formatDateTime(task.closedAt)}`} />
      )}
      {task?.state === 'closed' && (
        <Alert type="success" showIcon message="盘点已关单，位置不符样本已一次性换位并调整两边容器占用" description={`关单人：${task.closedByName}，时间：${formatDateTime(task.closedAt)}`} />
      )}
      {driftHint && (
        <Alert type="info" showIcon message="盘点期间若有样本已完成交接，关单会被拦截，旧清单不会覆盖现位置；该任务只能取消后对容器重新盘点。" />
      )}

      <Row gutter={16}>
        <Col span={4}><Card><Statistic title="清单总数" value={task?.totalCount ?? 0} /></Card></Col>
        <Col span={4}><Card><Statistic title="待标记" value={task?.pendingCount ?? 0} valueStyle={{ color: '#1677ff' }} /></Card></Col>
        <Col span={4}><Card><Statistic title="在库" value={task?.inStockCount ?? 0} valueStyle={{ color: '#3f8600' }} /></Card></Col>
        <Col span={4}><Card><Statistic title="缺失" value={task?.missingCount ?? 0} valueStyle={{ color: '#cf1322' }} /></Card></Col>
        <Col span={4}><Card><Statistic title="位置不符" value={task?.mismatchCount ?? 0} valueStyle={{ color: '#d48806' }} /></Card></Col>
        <Col span={4}><Card><Statistic title="完成进度" value={percent} suffix="%" /></Card></Col>
      </Row>
      <Progress percent={percent} status={task?.pendingCount === 0 ? 'success' : 'active'} strokeColor={{ from: '#1677ff', to: '#52c41a' }} />

      <Card size="small" title="任务信息">
        <Descriptions column={{ xs: 1, sm: 2, md: 3 }} size="small">
          <Descriptions.Item label="发起人">{task?.startedByName}</Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatDateTime(task?.startedAt)}</Descriptions.Item>
          <Descriptions.Item label="容器在库量">{task?.container ? `${task.container.occupied} / ${task.container.capacity}` : '-'}</Descriptions.Item>
          <Descriptions.Item label="任务备注" span={3}>{task?.note || '-'}</Descriptions.Item>
        </Descriptions>
      </Card>

      <div className="table-toolbar">
        <Input allowClear prefix={<SearchOutlined />} placeholder="搜索接收号、格位或说明" value={search} onChange={(event) => { setPage(1); setSearch(event.target.value) }} onPressEnter={() => void loadItems()} />
        <Select style={{ width: 150 }} value={resultFilter} onChange={(value) => { setPage(1); setResultFilter(value) }} options={[{ value: '', label: '全部条目' }, { value: 'pending', label: '待核对' }, { value: 'in_stock', label: '在库' }, { value: 'missing', label: '缺失' }, { value: 'mismatched', label: '位置不符' }]} />
        <Button onClick={() => void loadItems()}>查询</Button>
      </div>
      <Table<StocktakeItem>
        rowKey="id"
        columns={columns}
        dataSource={items}
        loading={loading}
        size="middle"
        scroll={{ x: 'max-content' }}
        pagination={{ current: page, pageSize, total, showSizeChanger: true, onChange: (nextPage, nextSize) => { setPage(nextPage); setPageSize(nextSize) } }}
      />

      <Modal
        title={`标记盘点结果 · ${markTarget?.accessionNo || ''}`}
        open={Boolean(markTarget)}
        confirmLoading={saving}
        onOk={() => void submitMark()}
        onCancel={() => setMarkTarget(null)}
        okText="保存结果"
        cancelText="取消"
      >
        <Descriptions size="small" column={1} bordered style={{ marginBottom: 16 }}>
          <Descriptions.Item label="原容器 / 格位">
            {markTarget?.originContainer?.code || markTarget?.originContainerId} / {markTarget?.originPosition}
          </Descriptions.Item>
        </Descriptions>
        <Form form={markForm} layout="vertical" onValuesChange={(changed) => {
          if ('result' in changed) setMarkResult(changed.result as StocktakeResult)
        }}>
          <Form.Item name="result" label="核对结果" rules={[{ required: true }]}>
            <Select options={[{ value: 'in_stock', label: '在库（实物与清单一致）' }, { value: 'missing', label: '缺失（实物不在）' }, { value: 'mismatched', label: '位置不符（实物在其他容器/格位）' }]} />
          </Form.Item>
          {markResult === 'missing' && (
            <Form.Item name="remark" label="缺失说明" rules={[{ required: true, min: 3, max: 1000, message: '缺失必须填写至少 3 个字符的说明' }]}>
              <Input.TextArea rows={3} placeholder="例如：架位空缺，最后一次取出记录为 9 月 12 日" showCount />
            </Form.Item>
          )}
          {markResult === 'mismatched' && (
            <>
              <Alert type="info" showIcon style={{ marginBottom: 12 }} message="新位置仅在关单时一次性生效，期间可反复修改" />
              <Row gutter={12}>
                <Col span={12}>
                  <Form.Item name="newContainerId" label="新容器" rules={[{ required: true, message: '请选择新容器' }]}>
                    <Select showSearch optionFilterProp="label" placeholder="选择可用容器" options={containers.map((item) => ({ value: item.id, label: `${item.code} · ${item.name}` }))} />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item name="newPosition" label="新格位" rules={[{ required: true, message: '请填写新格位' }, { max: 120 }]}>
                    <Input placeholder="R05-BX02-C11" />
                  </Form.Item>
                </Col>
              </Row>
              <Form.Item name="remark" label="不符说明" rules={[{ max: 1000 }]}>
                <Input.TextArea rows={2} placeholder="可选：记录发现过程或经手信息" showCount />
              </Form.Item>
            </>
          )}
          {markResult === 'in_stock' && (
            <Form.Item name="remark" label="备注" rules={[{ max: 1000 }]}>
              <Input.TextArea rows={2} maxLength={1000} showCount placeholder="可选" />
            </Form.Item>
          )}
        </Form>
      </Modal>
    </div>
  )
}
