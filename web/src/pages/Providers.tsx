import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Tag, Space, Popconfirm, message, Result } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Provider as ProviderType } from '../api/providers'
import {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  testProvider,
} from '../api/providers'

const protocolColors: Record<string, string> = { openai: 'blue', claude: 'purple' }

export default function Providers() {
  const qc = useQueryClient()
  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: listProviders,
  })

  const createMut = useMutation({
    mutationFn: createProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ProviderType> }) => updateProvider(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteMut = useMutation({
    mutationFn: deleteProvider,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['providers'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该 API 源下存在模型，无法删除')
      else message.error('删除失败')
    },
  })

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ProviderType | null>(null)
  const [form] = Form.useForm()

  // Test connectivity state
  const [testOpen, setTestOpen] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; status: number; error?: string } | null>(null)
  const [testLoading, setTestLoading] = useState(false)

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ protocol: 'openai', enabled: true })
    setModalOpen(true)
  }

  const openEdit = (p: ProviderType) => {
    setEditing(p)
    form.setFieldsValue(p)
    setModalOpen(true)
  }

  const handleSubmit = async () => {
    const vals = await form.validateFields()
    if (editing) {
      updateMut.mutate({ id: editing.id, data: vals })
    } else {
      createMut.mutate(vals)
    }
    setModalOpen(false)
  }

  const handleTest = async (id: number) => {
    setTestLoading(true)
    setTestOpen(true)
    setTestResult(null)
    try {
      const r = await testProvider(id)
      setTestResult(r)
    } catch {
      setTestResult({ ok: false, status: 0, error: '网络错误' })
    } finally {
      setTestLoading(false)
    }
  }

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Base URL', dataIndex: 'base_url', key: 'base_url', ellipsis: true },
    {
      title: '协议', dataIndex: 'protocol', key: 'protocol',
      render: (p: string) => <Tag color={protocolColors[p] ?? 'default'}>{p}</Tag>,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ProviderType) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ProviderType) => (
        <Space>
          <Button size="small" onClick={() => handleTest(r.id)}>测试</Button>
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加 API 源</Button>
      </div>
      <Table columns={columns} dataSource={providers} rowKey="id" loading={isLoading} />

      <Modal
        title={editing ? '编辑 API 源' : '添加 API 源'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createMut.isPending || updateMut.isPending}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true, message: '请输入地址' }]}>
            <Input placeholder="https://api.openai.com/v1" />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" rules={[{ required: !editing, message: '请输入密钥' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="protocol" label="协议" rules={[{ required: true }]}>
            <Select options={[{ value: 'openai', label: 'OpenAI' }, { value: 'claude', label: 'Claude' }]} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="连通性测试"
        open={testOpen}
        footer={<Button onClick={() => setTestOpen(false)}>关闭</Button>}
        onCancel={() => setTestOpen(false)}
      >
        {testLoading && <p>正在测试...</p>}
        {testResult && (
          <Result
            status={testResult.ok ? 'success' : 'error'}
            title={testResult.ok ? '连接成功' : '连接失败'}
            subTitle={testResult.ok ? `HTTP ${testResult.status}` : (testResult.error ?? `HTTP ${testResult.status}`)}
          />
        )}
      </Modal>
    </div>
  )
}
