import { useState } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Space, Popconfirm, message, Empty, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  listGroups, createGroup, updateGroup, deleteGroup,
  listGroupItems, replaceGroupItems, type ModelGroup,
} from '../api/groups'
import { listModels, type Model } from '../api/models'

export default function Queues() {
  const qc = useQueryClient()
  const { data: groups, isLoading } = useQuery({ queryKey: ['groups'], queryFn: listGroups })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: listModels })

  const [editOpen, setEditOpen] = useState(false)
  const [editing, setEditing] = useState<ModelGroup | null>(null)
  const [form] = Form.useForm()

  const [memberOpen, setMemberOpen] = useState(false)
  const [memberGroupId, setMemberGroupId] = useState<number | null>(null)
  const [picked, setPicked] = useState<number[]>([])

  const createMut = useMutation({
    mutationFn: createGroup,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('创建成功') },
    onError: () => message.error('创建失败'),
  })
  const updateMut = useMutation({
    mutationFn: (p: { id: number; data: Partial<ModelGroup> }) => updateGroup(p.id, p.data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('更新成功') },
    onError: () => message.error('更新失败'),
  })
  const deleteMut = useMutation({
    mutationFn: deleteGroup,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('已删除') },
    onError: (err: any) => err?.response?.status === 409 ? message.warning('默认队列无法删除') : message.error('删除失败'),
  })
  const replaceMut = useMutation({
    mutationFn: (p: { id: number; ids: number[] }) => replaceGroupItems(p.id, p.ids),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups'] }); message.success('已保存成员') },
    onError: () => message.error('保存失败'),
  })

  const openCreate = () => { setEditing(null); form.resetFields(); form.setFieldsValue({ enabled: true }); setEditOpen(true) }
  const openEdit = (g: ModelGroup) => { setEditing(g); form.setFieldsValue(g); setEditOpen(true) }
  const submit = async () => {
    const vals = await form.validateFields()
    try {
      if (editing) await updateMut.mutateAsync({ id: editing.id, data: vals })
      else await createMut.mutateAsync(vals)
      setEditOpen(false)
    } catch { /* msg shown by mutation */ }
  }

  const openMembers = async (g: ModelGroup) => {
    setMemberGroupId(g.id)
    const items = await listGroupItems(g.id)
    setPicked(items.map((i) => i.model_id))
    setMemberOpen(true)
  }
  const saveMembers = async () => {
    if (memberGroupId === null) return
    try { await replaceMut.mutateAsync({ id: memberGroupId, ids: picked }); setMemberOpen(false) } catch { /* msg */ }
  }

  const enabledModels = (models ?? []).filter((m) => m.enabled)
  const pickedModels = picked.map((id) => enabledModels.find((m) => m.id === id)).filter(Boolean) as Model[]
  const available = enabledModels.filter((m) => !picked.includes(m.id))

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '展示名', dataIndex: 'display_name', key: 'display_name' },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
    { title: '模型数', dataIndex: 'item_count', key: 'item_count', width: 90 },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled', width: 80,
      render: (_: boolean, r: ModelGroup) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { ...r, enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: unknown, r: ModelGroup) => (
        <Space>
          <Button size="small" onClick={() => openMembers(r)}>管理成员</Button>
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const move = (idx: number, dir: -1 | 1) => {
    const next = [...picked]
    const j = idx + dir
    if (j < 0 || j >= next.length) return
    ;[next[idx], next[j]] = [next[j], next[idx]]
    setPicked(next)
  }

  return (
    <div>
      <div className="page-title">模型队列</div>
      <div className="page-subtitle">聚合多个模型为具名队列,按序失败转移;队列是对外唯一可路由目标</div>
      <div style={{ marginBottom: 12 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加队列</Button>
      </div>
      <Table columns={columns} dataSource={groups} rowKey="id" loading={isLoading} locale={{ emptyText: <Empty description="暂无队列" /> }} />

      <Modal title={editing ? '编辑队列' : '添加队列'} open={editOpen} onOk={submit} onCancel={() => setEditOpen(false)} confirmLoading={createMut.isPending || updateMut.isPending}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如 deepseek-v4-flash" />
          </Form.Item>
          <Form.Item name="display_name" label="展示名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述" tooltip="给判定模型看的能力描述">
            <Input.TextArea />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>

      <Modal title="管理队列成员" open={memberOpen} onOk={saveMembers} onCancel={() => setMemberOpen(false)} width={560} confirmLoading={replaceMut.isPending}>
        <div style={{ display: 'flex', gap: 16 }}>
          <div style={{ flex: 1 }}>
            <div style={{ marginBottom: 8, fontWeight: 600 }}>可选模型</div>
            <Select
              style={{ width: '100%' }}
              placeholder="选择模型添加"
              value={undefined}
              options={available.map((m) => ({ value: m.id, label: m.name }))}
              onChange={(id) => { if (id !== undefined && !picked.includes(id)) setPicked([...picked, id]) }}
            />
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ marginBottom: 8, fontWeight: 600 }}>队列成员(顺序即请求顺序)</div>
            {pickedModels.length === 0 ? (
              <Empty description="未添加成员" />
            ) : pickedModels.map((m, idx) => (
              <div key={m.id} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '4px 0' }}>
                <Tag>{idx + 1}</Tag>
                <span style={{ flex: 1 }}>{m.name}</span>
                <Space size="small">
                  <Button size="small" disabled={idx === 0} onClick={() => move(idx, -1)}>↑</Button>
                  <Button size="small" disabled={idx === pickedModels.length - 1} onClick={() => move(idx, 1)}>↓</Button>
                  <Button size="small" danger onClick={() => setPicked(picked.filter((x) => x !== m.id))}>移除</Button>
                </Space>
              </div>
            ))}
          </div>
        </div>
      </Modal>
    </div>
  )
}
