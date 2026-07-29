import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Tag, Space, Popconfirm, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import type { Model as ModelType } from '../api/models'
import type { Provider } from '../api/providers'
import { listModels, createModel, updateModel, deleteModel, setJudgeModel } from '../api/models'
import { listProviders } from '../api/providers'

export default function Models() {
  const qc = useQueryClient()
  const { data: models, isLoading } = useQuery({ queryKey: ['models'], queryFn: listModels })
  const { data: providers } = useQuery({ queryKey: ['providers'], queryFn: listProviders })

  const createMut = useMutation({
    mutationFn: createModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('创建成功') },
    onError: () => { message.error('创建失败') },
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelType> }) => updateModel(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('更新成功') },
    onError: () => { message.error('更新失败') },
  })
  const deleteMut = useMutation({
    mutationFn: deleteModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('已删除') },
    onError: (err: any) => {
      if (err?.response?.status === 409) message.warning('该模型正被路由配置引用，无法删除')
      else message.error('删除失败')
    },
  })
  const judgeMut = useMutation({
    mutationFn: setJudgeModel,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['models'] }); message.success('已设置判定模型') },
    onError: () => { message.error('设置失败') },
  })

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ModelType | null>(null)
  const [form] = Form.useForm()

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ enabled: true })
    setModalOpen(true)
  }

  const openEdit = (m: ModelType) => {
    setEditing(m)
    form.setFieldsValue(m)
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

  const providerMap = new Map((providers ?? []).map((p: Provider) => [p.id, p.name]))

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '显示名', dataIndex: 'display_name', key: 'display_name' },
    {
      title: 'API 源', dataIndex: 'provider_id', key: 'provider_id',
      render: (id: number) => providerMap.get(id) ?? '-',
    },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    {
      title: '判定', dataIndex: 'is_judge', key: 'is_judge',
      render: (v: boolean) => v ? <Tag color="cyan">判定模型</Tag> : null,
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled',
      render: (_: boolean, r: ModelType) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelType) => (
        <Space>
          {!r.is_judge && (
            <Popconfirm title="设为判定模型？" onConfirm={() => judgeMut.mutate(r.id)}>
              <Button size="small">设为判定</Button>
            </Popconfirm>
          )}
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
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加模型</Button>
      </div>
      <Table
        columns={columns}
        dataSource={models}
        rowKey="id"
        loading={isLoading}
        rowClassName={(r) => r.is_judge ? 'ant-table-row-selected' : ''}
        onRow={(r) => ({
          style: r.is_judge ? { backgroundColor: '#e6fffb' } : undefined,
        })}
      />

      <Modal
        title={editing ? '编辑模型' : '添加模型'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createMut.isPending || updateMut.isPending}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="发给上游的真实 model id" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="provider_id" label="API 源" rules={[{ required: true }]}>
            <Select
              options={(providers ?? []).filter((p: Provider) => p.enabled).map((p: Provider) => ({
                value: p.id,
                label: p.name,
              }))}
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
    </div>
  )
}
