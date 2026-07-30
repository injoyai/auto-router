import { useEffect } from 'react'
import { Card, Form, Select, Switch, InputNumber, Button, message, Spin, Tooltip } from 'antd'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRoutingConfig, updateRoutingConfig } from '../api/routing'
import { listModels } from '../api/models'

export default function Routing() {
  const qc = useQueryClient()
  const [form] = Form.useForm()

  const { data: cfg, isLoading } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })

  useEffect(() => {
    if (cfg) form.setFieldsValue(cfg)
  }, [cfg, form])

  const saveMut = useMutation({
    mutationFn: updateRoutingConfig,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['routingConfig'] })
      message.success('保存成功')
    },
    onError: () => { message.error('保存失败') },
  })

  const handleSave = async () => {
    const vals = await form.validateFields()
    saveMut.mutate(vals)
  }

  if (isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 48 }} />

  const modelOptions = (models ?? []).filter((m) => m.enabled).map((m) => ({ value: m.id, label: `${m.display_name} (${m.name})` }))

  return (
    <Card title="路由配置" className="mono-card" style={{ maxWidth: 600 }}>
      <Form form={form} layout="vertical" style={{ maxWidth: 500 }}>
        <Form.Item name="judge_model_id" label="判定模型">
          <Select allowClear placeholder="选择判定模型" options={modelOptions} />
        </Form.Item>
        <Form.Item name="default_model_id" label="默认兜底模型">
          <Select allowClear placeholder="选择兜底模型" options={modelOptions} />
        </Form.Item>
        <Form.Item name="enable_next_model_directive" label="允许模型回选" valuePropName="checked">
          <Tooltip title="开启后，模型可在回复中用 <<next_model: 模型名>> 指定下一轮模型">
            <Switch />
          </Tooltip>
        </Form.Item>
        <Form.Item name="session_ttl_seconds" label="会话 TTL（秒）">
          <InputNumber min={60} max={86400} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="judge_max_input_chars" label="判定输入截断（字符）">
          <InputNumber min={100} max={10000} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item>
          <Button type="primary" onClick={handleSave} loading={saveMut.isPending}>保存</Button>
        </Form.Item>
      </Form>
    </Card>
  )
}
