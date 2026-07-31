import { useState, useEffect } from 'react'
import { Table, Button, Switch, Modal, Form, Input, InputNumber, Select, Tag, Space, Popconfirm, message, Result, Empty, Spin } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Provider as ProviderType } from '../api/providers'
import type { Model as ModelType, ModelTestResult } from '../api/models'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
} from '../api/providers'
import {
  listModels,
  createModel,
  updateModel,
  deleteModel,
  testModel,
} from '../api/models'
import { getRoutingConfig } from '../api/routing'

const protocolColors: Record<string, string> = { openai: 'blue', claude: 'purple' }

export default function Sources() {
  const qc = useQueryClient()
  const { data: providers, isLoading: provLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })

  const [selectedProviderId, setSelectedProviderId] = useState<number | null>(null)

  const { data: models, isLoading: modelsLoading } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
    enabled: selectedProviderId !== null,
  })

  // 判定模型统一从 routingConfig 派生（单一数据源，避免与路由配置页冲突）
  const { data: routingConfig } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
  })
  const judgeModelId = routingConfig?.judge_model_id ?? null

  // Provider mutations
  const createProvMut = useMutation({
    mutationFn: createProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateProvMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ProviderType> }) => updateProvider(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteProvMut = useMutation({
    mutationFn: deleteProvider,
    onSuccess: (_data, deletedId) => {
      qc.invalidateQueries({ queryKey: ['providers'] })
      message.success('已删除')
      if (deletedId === selectedProviderId) {
        const remaining = providers?.filter((p) => p.id !== deletedId)
        setSelectedProviderId(remaining && remaining.length > 0 ? remaining[0].id : null)
      }
    },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该 API 源下存在模型，无法删除')
      else message.error('删除失败')
    },
  })

  // Model mutations
  const createModelMut = useMutation({
    mutationFn: createModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateModelMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelType> }) => updateModel(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteModelMut = useMutation({
    mutationFn: deleteModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该模型正被路由配置引用，无法删除')
      else message.error('删除失败')
    },
  })

  // Provider modal state
  const [provModalOpen, setProvModalOpen] = useState(false)
  const [editingProv, setEditingProv] = useState<ProviderType | null>(null)
  const [provForm] = Form.useForm()

  // Model modal state
  const [modelModalOpen, setModelModalOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<ModelType | null>(null)
  const [modelForm] = Form.useForm()

  // Model test modal state
  const [modelTestOpen, setModelTestOpen] = useState(false)
  const [modelTestResult, setModelTestResult] = useState<ModelTestResult | null>(null)
  const [modelTestLoading, setModelTestLoading] = useState(false)

  // Auto-select first provider when list loads
  useEffect(() => {
    if (providers && providers.length > 0 && selectedProviderId === null) {
      setSelectedProviderId(providers[0].id)
    }
  }, [providers, selectedProviderId])

  const selectedProvider = providers?.find((p) => p.id === selectedProviderId)
  const filteredModels = (models ?? []).filter((m) => m.provider_id === selectedProviderId)

  // Provider modal handlers
  const openCreateProv = () => {
    setEditingProv(null)
    provForm.resetFields()
    provForm.setFieldsValue({ protocol: 'openai', enabled: true })
    setProvModalOpen(true)
  }
  const openEditProv = (p: ProviderType) => {
    setEditingProv(p)
    provForm.setFieldsValue({
      ...p,
      api_key: p.has_api_key ? '********' : '',
    })
    setProvModalOpen(true)
  }
  const handleProvSubmit = async () => {
    const vals = await provForm.validateFields()
    // 占位符不提交，后端空值会保留原 key
    if (vals.api_key === '********') {
      vals.api_key = ''
    }
    try {
      if (editingProv) {
        await updateProvMut.mutateAsync({ id: editingProv.id, data: vals })
      } else {
        await createProvMut.mutateAsync(vals)
      }
      setProvModalOpen(false)
    } catch {
      // mutation onError already shows message, modal stays open
    }
  }

  // Model modal handlers
  const openCreateModel = () => {
    if (!selectedProviderId) {
      message.warning('请先选择 API 源')
      return
    }
    setEditingModel(null)
    modelForm.resetFields()
    modelForm.setFieldsValue({ provider_id: selectedProviderId, enabled: true })
    setModelModalOpen(true)
  }
  const openEditModel = (m: ModelType) => {
    setEditingModel(m)
    modelForm.setFieldsValue(m)
    setModelModalOpen(true)
  }
  const handleModelSubmit = async () => {
    const vals = await modelForm.validateFields()
    try {
      if (editingModel) {
        await updateModelMut.mutateAsync({ id: editingModel.id, data: vals })
      } else {
        await createModelMut.mutateAsync(vals)
      }
      setModelModalOpen(false)
    } catch {
      // mutation onError already shows message, modal stays open
    }
  }

  // Model test handler
  const handleModelTest = async (id: number) => {
    setModelTestLoading(true)
    setModelTestOpen(true)
    setModelTestResult(null)
    try {
      const r = await testModel(id)
      setModelTestResult(r)
    } catch {
      setModelTestResult({ ok: false, status: 0, latency_ms: 0, error: '网络错误' })
    } finally {
      setModelTestLoading(false)
    }
  }

  const modelColumns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '显示名', dataIndex: 'display_name', key: 'display_name' },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    {
      title: '判定', key: 'is_judge',
      render: (_: unknown, r: ModelType) => judgeModelId === r.id ? <Tag color="blue">判定模型</Tag> : null,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ModelType) => (
        <Switch checked={r.enabled} onChange={(v) => updateModelMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelType) => (
        <Space>
          <Button size="small" onClick={() => handleModelTest(r.id)}>测试</Button>
          <Button size="small" onClick={() => openEditModel(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteModelMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div className="page-title">模型管理</div>
      <div className="page-subtitle">管理 API 源与模型配置</div>
      <div style={{ display: 'flex', gap: 16, minHeight: 'calc(100vh - 160px)' }}>
        {/* Left: Provider list */}
        <div style={{ width: 280, flexShrink: 0 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <strong style={{ fontFamily: "'Sora', sans-serif", fontSize: 15 }}>API 源</strong>
            <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreateProv}>添加</Button>
          </div>
          {provLoading ? (
            <Spin />
          ) : providers && providers.length > 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {providers.map((p) => (
                <div
                  key={p.id}
                  className={`provider-item ${p.id === selectedProviderId ? 'provider-item--active' : ''}`}
                  onClick={() => setSelectedProviderId(p.id)}
                  style={{ opacity: p.enabled ? 1 : 0.5 }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span style={{ fontWeight: p.id === selectedProviderId ? 700 : 500 }}>{p.name}</span>
                    <Tag color={protocolColors[p.protocol] ?? 'default'} style={{ marginRight: 0 }}>{p.protocol}</Tag>
                  </div>
                  <Space size="small" style={{ marginTop: 4 }} onClick={(e) => e.stopPropagation()}>
                    <Button size="small" type="link" style={{ padding: 0 }} onClick={() => openEditProv(p)}>编辑</Button>
                    <Popconfirm title="确认删除？" onConfirm={() => deleteProvMut.mutate(p.id)}>
                      <Button size="small" type="link" danger style={{ padding: 0 }}>删除</Button>
                    </Popconfirm>
                  </Space>
                </div>
              ))}
            </div>
          ) : (
            <Empty description="暂无 API 源" />
          )}
        </div>

        {/* Right: Model table */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <strong style={{ fontFamily: "'Sora', sans-serif", fontSize: 15 }}>{selectedProvider ? `${selectedProvider.name} 的模型` : '模型'}</strong>
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModel} disabled={!selectedProviderId}>
              添加模型
            </Button>
          </div>
          <Table
            columns={modelColumns}
            dataSource={filteredModels}
            rowKey="id"
            loading={modelsLoading}
            rowClassName={(r) => judgeModelId === r.id ? 'ant-table-row-selected' : ''}
            onRow={(r) => ({
              style: judgeModelId === r.id ? { backgroundColor: '#eef2ff' } : undefined,
            })}
            locale={{ emptyText: selectedProviderId ? '暂无模型' : '请先选择左侧的 API 源' }}
          />
        </div>

        {/* Provider Modal */}
        <Modal
          title={editingProv ? '编辑 API 源' : '添加 API 源'}
          open={provModalOpen}
          onOk={handleProvSubmit}
          onCancel={() => setProvModalOpen(false)}
          confirmLoading={createProvMut.isPending || updateProvMut.isPending}
        >
          <Form form={provForm} layout="vertical">
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="base_url" label="Base URL" rules={[{ required: true, message: '请输入地址' }]}>
              <Input placeholder="https://api.openai.com/v1" />
            </Form.Item>
            <Form.Item name="api_key" label="API Key" rules={[{ required: !editingProv, message: '请输入密钥' }]}>
              <Input.Password placeholder={editingProv ? '留空保持不变' : '请输入密钥'} />
            </Form.Item>
            <Form.Item name="protocol" label="协议" rules={[{ required: true }]}>
              <Select options={[{ value: 'openai', label: 'OpenAI' }, { value: 'claude', label: 'Claude' }]} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="retry_max" label="重试次数" tooltip="失败后重试次数，0 表示不重试">
              <InputNumber min={0} max={5} style={{ width: '100%' }} placeholder="0" />
            </Form.Item>
            <Form.Item name="retry_backoff_ms" label="退避基数(ms)" tooltip="指数退避基数，如 500 -> 500ms/1s/2s">
              <InputNumber min={100} max={5000} step={100} style={{ width: '100%' }} placeholder="500" />
            </Form.Item>
          </Form>
        </Modal>

        {/* Model Modal */}
        <Modal
          title={editingModel ? '编辑模型' : '添加模型'}
          open={modelModalOpen}
          onOk={handleModelSubmit}
          onCancel={() => setModelModalOpen(false)}
          confirmLoading={createModelMut.isPending || updateModelMut.isPending}
        >
          <Form form={modelForm} layout="vertical">
            <Form.Item name="name" label="名称" rules={[{ required: true }]}>
              <Input placeholder="发给上游的真实 model id" />
            </Form.Item>
            <Form.Item name="display_name" label="显示名" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="provider_id" label="API 源" rules={[{ required: true }]}>
              <Select
                disabled
                options={(providers ?? []).filter((p) => p.enabled).map((p) => ({ value: p.id, label: p.name }))}
              />
            </Form.Item>
            <Form.Item name="description" label="描述">
              <Input.TextArea placeholder="给判定模型看的描述" />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Form>
        </Modal>

        {/* Model Test Modal */}
        <Modal
          title="模型测试"
          open={modelTestOpen}
          footer={<Button onClick={() => setModelTestOpen(false)}>关闭</Button>}
          onCancel={() => setModelTestOpen(false)}
        >
          {modelTestLoading && <p>正在测试...</p>}
          {modelTestResult && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <Result
                status={modelTestResult.ok ? 'success' : 'error'}
                title={modelTestResult.ok ? '模型可用' : '模型不可用'}
                subTitle={modelTestResult.ok ? `耗时 ${modelTestResult.latency_ms}ms` : (modelTestResult.error ?? `HTTP ${modelTestResult.status}`)}
                style={{ padding: '12px 0' }}
              />
              {modelTestResult.ok && modelTestResult.usage && (
                <div style={{ display: 'flex', gap: 24, justifyContent: 'center', fontSize: 13, color: '#6a604c' }}>
                  <span><strong style={{ color: '#4e4636' }}>提示:</strong> {modelTestResult.usage.prompt_tokens}</span>
                  <span><strong style={{ color: '#4e4636' }}>补全:</strong> {modelTestResult.usage.completion_tokens}</span>
                  <span><strong style={{ color: '#4e4636' }}>合计:</strong> {modelTestResult.usage.total_tokens}</span>
                </div>
              )}
              {modelTestResult.ok && modelTestResult.reply && (
                <div>
                  <div style={{ fontSize: 12, color: '#8a7f66', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.04em' }}>模型回复</div>
                  <pre style={{ whiteSpace: 'pre-wrap', fontFamily: "'JetBrains Mono', monospace", fontSize: 13, background: '#f8f9fc', padding: 12, borderRadius: 10, border: '1px solid #f1f2f8', margin: 0 }}>
                    {modelTestResult.reply}
                  </pre>
                </div>
              )}
            </div>
          )}
        </Modal>
      </div>
    </div>
  )
}
