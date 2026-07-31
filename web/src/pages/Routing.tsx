import { useEffect, useState } from 'react'
import { Card, Form, Select, InputNumber, Button, message, Spin, Input, Space, Typography, Tooltip, Tag } from 'antd'
import { InfoCircleOutlined, CopyOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRoutingConfig, updateRoutingConfig } from '../api/routing'
import { listModels } from '../api/models'
import { listGroups } from '../api/groups'

const { Text } = Typography

export default function Routing() {
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [apiKey, setApiKey] = useState('')

  const { data: cfg, isLoading } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
  })

  const { data: models } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })

  const { data: groups } = useQuery({
    queryKey: ['groups'],
    queryFn: listGroups,
  })

  useEffect(() => {
    if (cfg) {
      form.setFieldsValue(cfg)
      setApiKey(cfg.gateway_token || '')
    }
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
    saveMut.mutate({ ...vals, gateway_token: apiKey })
  }

  if (isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 48 }} />

  const enabledModels = (models ?? []).filter((m) => m.enabled)
  const modelOptions = enabledModels.map((m) => ({ value: m.id, label: m.name }))
  const enabledGroups = (groups ?? []).filter((g) => g.enabled)
  const groupOptions = enabledGroups.map((g) => ({ value: g.id, label: g.name }))
  const baseUrl = window.location.origin + '/v1'

  const copyText = (text: string, label: string) => {
    navigator.clipboard.writeText(text)
    message.success(`${label}已复制`)
  }

  // 可用模型名称列表（含 auto）- 对外暴露的是队列名
  const modelNames = ['auto', ...enabledGroups.map((g) => g.name)]

  return (
    <div>
      <div className="page-title">路由配置</div>
      <div className="page-subtitle">配置智能路由的判定模型、兜底策略与 API Key</div>

      {/* 请求地址 & 可用模型 */}
      <Card className="aurora-card" style={{ maxWidth: 600, marginBottom: 20 }}>
        <Typography.Title level={5} style={{ marginBottom: 16 }}>请求地址</Typography.Title>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            value={baseUrl}
            readOnly
            style={{ borderRadius: '8px 0 0 8px', fontFamily: 'var(--font-mono)', fontSize: 13 }}
          />
          <Button
            icon={<CopyOutlined />}
            onClick={() => copyText(baseUrl, '请求地址')}
            style={{ borderRadius: '0 8px 8px 0', width: 44 }}
          />
        </Space.Compact>
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
          复制此地址填入第三方工具的 Base URL,工具会自动拼接 <Text code>/chat/completions</Text>(OpenAI)或 <Text code>/messages</Text>(Claude)端点
        </Text>

        <div style={{ height: 1, background: 'var(--sand-100)', margin: '20px 0' }} />

        <Typography.Title level={5} style={{ marginBottom: 12 }}>可用模型名称</Typography.Title>
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 12 }}>
          客户端可在请求的 <Text code>model</Text> 字段中指定以下名称，<Text code>auto</Text> 表示由网关智能路由
        </Text>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {modelNames.map((name) => (
            <Tag
              key={name}
              style={{
                cursor: 'pointer',
                padding: '4px 12px',
                fontSize: 13,
                fontFamily: name === 'auto' ? 'var(--font-body)' : 'var(--font-mono)',
                background: name === 'auto' ? 'var(--primary-light)' : 'var(--cream)',
                color: name === 'auto' ? 'var(--primary)' : 'var(--sand-700)',
                borderRadius: 8,
              }}
              onClick={() => copyText(name, `模型名 ${name}`)}
            >
              {name === 'auto' ? 'auto (智能路由)' : name}
              <CopyOutlined style={{ marginLeft: 6, fontSize: 11, opacity: 0.5 }} />
            </Tag>
          ))}
        </div>

        <div style={{ height: 1, background: 'var(--sand-100)', margin: '20px 0' }} />

        <Typography.Title level={5} style={{ marginBottom: 12 }}>API Key</Typography.Title>
        <Space.Compact style={{ width: '100%' }}>
          <Input.Password
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="API Key"
            style={{ borderRadius: '8px 0 0 8px' }}
          />
          <Button
            icon={<CopyOutlined />}
            onClick={() => apiKey && copyText(apiKey, 'API Key')}
            style={{ borderRadius: '0 8px 8px 0', width: 44 }}
          />
        </Space.Compact>
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
          客户端调用 /v1/* 接口时需在 Authorization 头中携带此 API Key，修改后保存生效
        </Text>
      </Card>

      <Card title="路由配置" className="aurora-card" style={{ maxWidth: 600 }}>
        <Form form={form} layout="vertical" style={{ maxWidth: 500 }}>
          <Form.Item
            name="judge_model_id"
            label={
              <Space size={4}>
                <span>判定模型</span>
                <Tooltip title="请选择非推理模型（如 deepseek-v4-flah），推理模型会消耗大量 token 用于思考，起不到节约成本的效果">
                  <InfoCircleOutlined style={{ color: 'var(--amber)', fontSize: 13 }} />
                </Tooltip>
              </Space>
            }
            extra={<Text type="secondary" style={{ fontSize: 12 }}>需要非推理模型，否则起不到节约 token 的效果</Text>}
          >
            <Select allowClear placeholder="选择判定模型" options={modelOptions} />
          </Form.Item>
          <Form.Item
            name="default_group_id"
            label={
              <Space size={4}>
                <span>默认兜底队列</span>
                <Tooltip title="当判定模型无法决策、或目标模型不可用时，请求将回退到此队列处理">
                  <InfoCircleOutlined style={{ color: 'var(--copper)', fontSize: 13 }} />
                </Tooltip>
              </Space>
            }
            extra={<Text type="secondary" style={{ fontSize: 12 }}>判定失败或目标模型不可用时，请求将回退到此队列</Text>}
          >
            <Select allowClear placeholder="选择兜底队列" options={groupOptions} />
          </Form.Item>
          <Form.Item name="judge_max_input_chars" label="判定输入截断（字符）">
            <InputNumber min={100} max={10000} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" onClick={handleSave} loading={saveMut.isPending} style={{ height: 42, paddingInline: 32 }}>保存</Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  )
}
