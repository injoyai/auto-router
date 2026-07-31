import { useState, useEffect, useRef } from 'react'
import { Table, Button, Switch, Modal, Form, Input, Select, Popconfirm, message, Empty } from 'antd'
import { PlusOutlined, CloseOutlined, HolderOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  listGroups, createGroup, updateGroup, deleteGroup,
  listGroupItems, replaceGroupItems, type ModelGroup,
} from '../api/groups'
import { listModels, type Model } from '../api/models'
import { listProviders, type Provider } from '../api/providers'

export default function Queues() {
  const qc = useQueryClient()
  const { data: groups, isLoading } = useQuery({ queryKey: ['groups'], queryFn: listGroups })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: listModels })
  const { data: providers } = useQuery({ queryKey: ['providers'], queryFn: listProviders })

  const [editOpen, setEditOpen] = useState(false)
  const [editing, setEditing] = useState<ModelGroup | null>(null)
  const [form] = Form.useForm()

  // 每个队列的已选模型 ID 列表
  const [rowModels, setRowModels] = useState<Record<number, number[]>>({})

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
    onSuccess: () => qc.invalidateQueries({ queryKey: ['groups'] }),
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

  const enabledModels = (models ?? []).filter((m) => m.enabled)
  const providerMap = new Map<number, Provider>((providers ?? []).map((p) => [p.id, p]))
  const providerName = (model: Model) => providerMap.get(model.provider_id)?.name ?? '-'
  const modelMap = new Map<number, Model>(enabledModels.map((m) => [m.id, m]))

  // 加载所有队列的成员
  useEffect(() => {
    if (!groups) return
    groups.forEach(async (g) => {
      if (rowModels[g.id]) return
      try {
        const items = await listGroupItems(g.id)
        const enabledIds = new Set(enabledModels.map((m) => m.id))
        const ids = items.map((i) => i.model_id).filter((id) => enabledIds.has(id))
        setRowModels((prev) => ({ ...prev, [g.id]: ids }))
      } catch { /* skip */ }
    })
  }, [groups])

  // 添加模型到队列
  const addModel = (g: ModelGroup, modelId: number) => {
    const current = rowModels[g.id] ?? []
    if (current.includes(modelId)) return
    const next = [...current, modelId]
    setRowModels((prev) => ({ ...prev, [g.id]: next }))
    replaceMut.mutate({ id: g.id, ids: next })
  }

  // 移除模型
  const removeModel = (g: ModelGroup, modelId: number) => {
    const current = rowModels[g.id] ?? []
    const next = current.filter((id) => id !== modelId)
    setRowModels((prev) => ({ ...prev, [g.id]: next }))
    replaceMut.mutate({ id: g.id, ids: next })
  }

  // 下拉框可选项：排除已选的
  const availableFor = (g: ModelGroup) => {
    const picked = new Set(rowModels[g.id] ?? [])
    return enabledModels.filter((m) => !picked.has(m.id))
  }

  // ---- 拖拽排序 ----
  const dragFrom = useRef<{ groupId: number; idx: number } | null>(null)
  const [dragOver, setDragOver] = useState<{ groupId: number; idx: number } | null>(null)

  const handleDragStart = (groupId: number, idx: number) => {
    dragFrom.current = { groupId, idx }
  }

  const handleDragOver = (e: React.DragEvent, groupId: number, idx: number) => {
    e.preventDefault()
    if (dragFrom.current?.groupId !== groupId) return
    setDragOver((prev) => prev?.idx === idx ? prev : { groupId, idx })
  }

  const handleDrop = (groupId: number) => {
    const from = dragFrom.current
    const over = dragOver
    if (!from || from.groupId !== groupId || !over || over.groupId !== groupId || from.idx === over.idx) {
      dragFrom.current = null
      setDragOver(null)
      return
    }
    const current = [...(rowModels[groupId] ?? [])]
    const [moved] = current.splice(from.idx, 1)
    current.splice(over.idx, 0, moved)
    setRowModels((prev) => ({ ...prev, [groupId]: current }))
    replaceMut.mutate({ id: groupId, ids: current })
    dragFrom.current = null
    setDragOver(null)
  }

  const handleDragEnd = () => {
    dragFrom.current = null
    setDragOver(null)
  }

  const columns = [
    {
      title: '名称', dataIndex: 'name', key: 'name', width: 180,
      render: (name: string, r: ModelGroup) => (
        <div>
          <div style={{ fontWeight: 500, fontSize: 14 }}>{name}</div>
          {r.remark && <div style={{ fontSize: 12, color: 'var(--sand-400)', marginTop: 2 }}>{r.remark}</div>}
        </div>
      ),
    },
    {
      title: '模型成员', key: 'members', width: '40%',
      render: (_: unknown, g: ModelGroup) => {
        const picked = rowModels[g.id] ?? []
        const available = availableFor(g)
        return (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {picked.map((id, idx) => {
              const m = modelMap.get(id)
              const isDragging = dragFrom.current?.groupId === g.id && dragFrom.current?.idx === idx
              const isDragOver = dragOver?.groupId === g.id && dragOver?.idx === idx
              return (
                <div
                  key={id}
                  className={`queue-member${isDragging ? ' queue-member-dragging' : ''}${isDragOver ? ' queue-member-drag-over' : ''}`}
                  draggable
                  onDragStart={() => handleDragStart(g.id, idx)}
                  onDragOver={(e) => handleDragOver(e, g.id, idx)}
                  onDrop={(e) => { e.preventDefault(); handleDrop(g.id) }}
                  onDragEnd={handleDragEnd}
                >
                  <div className="queue-member-handle">
                    <HolderOutlined style={{ fontSize: 12 }} />
                    <span style={{ fontSize: 11, fontWeight: 700, marginLeft: 2 }}>{idx + 1}</span>
                  </div>
                  <div className="queue-member-info">
                    <span className="queue-member-provider">{m ? providerName(m) : `#${id}`}</span>
                    <span className="queue-member-name">{m?.name ?? `#${id}`}</span>
                  </div>
                  <div className="queue-member-close" onClick={() => removeModel(g, id)}>
                    <CloseOutlined style={{ fontSize: 11 }} />
                  </div>
                </div>
              )
            })}
            {available.length > 0 && (
              <Select
                size="small"
                style={{ width: '100%', maxWidth: 280 }}
                placeholder={<span><PlusOutlined /> 添加模型</span>}
                value={undefined}
                listHeight={256}
                optionLabelProp="label"
                getPopupContainer={() => document.body}
                options={available.map((m) => ({
                  value: m.id,
                  label: m.name,
                  providerName: providerName(m),
                }))}
                optionRender={(option) => (
                  <div style={{ display: 'flex', flexDirection: 'column', lineHeight: '20px' }}>
                    <span style={{ fontSize: 11, color: 'var(--sand-400)' }}>{option.data?.providerName}</span>
                    <span style={{ fontWeight: 500 }}>{option.label}</span>
                  </div>
                )}
                onChange={(id) => { if (id !== undefined) addModel(g, id) }}
                suffixIcon={<PlusOutlined />}
              />
            )}
            {picked.length === 0 && available.length === 0 && (
              <span style={{ color: 'var(--sand-400)', fontSize: 13 }}>暂无可用模型</span>
            )}
          </div>
        )
      },
    },
    {
      title: '启用', dataIndex: 'enabled', key: 'enabled', width: 80, align: 'center' as const,
      render: (_: boolean, r: ModelGroup) => (
        <Switch checked={r.enabled} onChange={(v) => updateMut.mutate({ id: r.id, data: { name: r.name, remark: r.remark ?? '', enabled: v } })} />
      ),
    },
    {
      title: '操作', key: 'actions', width: 130, align: 'center' as const,
      render: (_: unknown, r: ModelGroup) => (
        <div style={{ display: 'flex', gap: 4, justifyContent: 'center' }}>
          <Button size="small" onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm title="确认删除？" onConfirm={() => deleteMut.mutate(r.id)}>
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </div>
      ),
    },
  ]

  return (
    <div>
      <div className="page-title">模型队列</div>
      <div className="page-subtitle">聚合多个模型为具名队列,按序失败转移;队列是对外唯一可路由目标</div>
      <div style={{ marginBottom: 12 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加队列</Button>
      </div>
      <Table
        columns={columns}
        dataSource={groups}
        rowKey="id"
        loading={isLoading}
        locale={{ emptyText: <Empty description="暂无队列" /> }}
        pagination={false}
      />

      <Modal title={editing ? '编辑队列' : '添加队列'} open={editOpen} onOk={submit} onCancel={() => setEditOpen(false)} confirmLoading={createMut.isPending || updateMut.isPending} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如 deepseek-v4-flash" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input placeholder="可选备注信息" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
