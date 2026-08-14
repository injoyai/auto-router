import { useEffect, useState } from 'react'
import { Card, Form, Select, Button, message, Spin, Input, Space, Typography, Tooltip, Tag } from 'antd'
import { InfoCircleOutlined, CopyOutlined, ReloadOutlined, ApiOutlined, KeyOutlined, CompassOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRoutingConfig, updateRoutingConfig } from '../api/routing'
import { listGroups } from '../api/groups'

const { Text } = Typography

// 生成随机 48 字符 hex token（与后端首次启动生成的格式一致）
function generateToken(): string {
  const bytes = new Uint8Array(24)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

export default function Routing() {
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [apiKey, setApiKey] = useState('')
  const [spinning, setSpinning] = useState(false)

  const { data: cfg, isLoading } = useQuery({
    queryKey: ['routingConfig'],
    queryFn: getRoutingConfig,
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

  // 保存路由策略（同时携带当前 API Key，避免覆盖）
  const handleSaveRouting = async () => {
    const vals = await form.validateFields()
    saveMut.mutate({ ...vals, gateway_token: apiKey })
  }

  // 保存 API Key（同时携带当前路由表单值，避免覆盖）
  const handleSaveApiKey = () => {
    saveMut.mutate({ ...form.getFieldsValue(), gateway_token: apiKey })
  }

  // 随机生成新 Key
  const handleRegenerate = () => {
    setSpinning(true)
    setApiKey(generateToken())
    message.info('已生成新 Key，点击保存生效')
    setTimeout(() => setSpinning(false), 500)
  }

  if (isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 48 }} />

  const enabledGroups = (groups ?? []).filter((g) => g.enabled)
  const groupOptions = enabledGroups.map((g) => ({ value: g.id, label: g.name }))
  const baseUrl = window.location.origin + '/v1'

  // 复制到剪贴板：优先 navigator.clipboard（HTTPS/localhost），非安全上下文或失败时
  // 回退到 document.execCommand('copy')（textarea 选中复制），避免局域网 IP 访问时无效
  const copyText = (text: string, label: string) => {
    const fallback = () => {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      let ok = false
      try {
        ok = document.execCommand('copy')
      } catch {
        ok = false
      }
      document.body.removeChild(ta)
      if (ok) {
        message.success(`${label}已复制`)
      } else {
        message.error(`${label}复制失败`)
      }
    }

    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(
        () => message.success(`${label}已复制`),
        () => fallback(),
      )
    } else {
      fallback()
    }
  }

  // 可用模型名称列表（含 auto）- 对外暴露的是队列名
  const modelNames = ['auto', ...enabledGroups.map((g) => g.name)]

  return (
    <div>
      <div className="page-title">路由配置</div>
      <div className="page-subtitle">配置智能路由的判定队列、兜底策略与 API Key</div>

      {/* ── 接入信息（全宽） ── */}
      <Card
        className="aurora-card aurora-fade-in"
        style={{ marginBottom: 20 }}
      >
        <Space size={8} style={{ marginBottom: 16 }}>
          <ApiOutlined style={{ color: 'var(--primary)', fontSize: 16 }} />
          <Typography.Title level={5} style={{ margin: 0 }}>请求地址</Typography.Title>
        </Space>
        <Space.Compact style={{ width: '100%', maxWidth: 520 }}>
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
          复制此地址填入第三方工具的 Base URL,工具会自动拼接 <Text code>/chat/completions</Text>(OpenAI)或 <Text code>/messages</Text>(Anthropic)端点
        </Text>

        <div style={{ height: 1, background: 'var(--sand-100)', margin: '20px 0' }} />

        <Space size={8} style={{ marginBottom: 12 }}>
          <Typography.Title level={5} style={{ margin: 0 }}>可用模型名称</Typography.Title>
        </Space>
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
      </Card>

      {/* ── 路由策略 + API Key 双列 ── */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 20 }}>
        <div style={{ flex: '1 1 480px', minWidth: 0 }}>
          <Card
            title={
              <Space size={8}>
                <CompassOutlined style={{ color: 'var(--primary)' }} />
                <span>路由策略</span>
              </Space>
            }
            className="aurora-card aurora-fade-in aurora-fade-in-1"
          >
            <Form form={form} layout="vertical">
              <Form.Item
                name="judge_group_id"
                label={
                  <Space size={4}>
                    <span>判定队列</span>
                    <Tooltip title="判定队列，按队列内模型顺序失败转移；建议选非推理模型组成的队列，推理模型会消耗大量 token 用于思考">
                      <InfoCircleOutlined style={{ color: 'var(--amber)', fontSize: 13 }} />
                    </Tooltip>
                  </Space>
                }
                extra={<Text type="secondary" style={{ fontSize: 12 }}>判定按队列内模型顺序失败转移，全部失败再回退兜底队列</Text>}
              >
                <Select allowClear placeholder="选择判定队列" options={groupOptions} />
              </Form.Item>
              <Form.Item
                name="default_group_id"
                label={
                  <Space size={4}>
                    <span>默认兜底队列</span>
                    <Tooltip title="当判定队列无法决策、或目标模型不可用时，请求将回退到此队列处理">
                      <InfoCircleOutlined style={{ color: 'var(--copper)', fontSize: 13 }} />
                    </Tooltip>
                  </Space>
                }
                extra={<Text type="secondary" style={{ fontSize: 12 }}>判定失败或目标模型不可用时，请求将回退到此队列</Text>}
              >
                <Select allowClear placeholder="选择兜底队列" options={groupOptions} />
              </Form.Item>
              <Form.Item>
                <Button type="primary" onClick={handleSaveRouting} loading={saveMut.isPending} style={{ height: 42, paddingInline: 32 }}>保存</Button>
              </Form.Item>
            </Form>
          </Card>
        </div>
        <div style={{ flex: '1 1 360px', minWidth: 0 }}>
          <Card
            title={
              <Space size={8}>
                <KeyOutlined style={{ color: 'var(--copper)' }} />
                <span>API Key</span>
                <Tag color="orange" style={{ margin: 0, fontSize: 11 }}>可修改</Tag>
              </Space>
            }
            className="aurora-card aurora-fade-in aurora-fade-in-2"
          >
            <Space.Compact style={{ width: '100%' }}>
              <Input.Password
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="API Key"
                style={{ borderRadius: '8px 0 0 8px', fontFamily: 'var(--font-mono)', fontSize: 13 }}
              />
              <Tooltip title="随机生成新 Key">
                <Button
                  icon={<ReloadOutlined spin={spinning} />}
                  onClick={handleRegenerate}
                  style={{ borderRadius: 0, width: 44 }}
                />
              </Tooltip>
              <Button
                icon={<CopyOutlined />}
                onClick={() => apiKey && copyText(apiKey, 'API Key')}
                style={{ borderRadius: '0 8px 8px 0', width: 44 }}
              />
            </Space.Compact>
            <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
              客户端调用 <Text code>/v1/*</Text> 接口时需在 <Text code>Authorization</Text> 头中携带此 Key。可手动编辑，或点击
              <ReloadOutlined style={{ margin: '0 4px', fontSize: 11, color: 'var(--copper)' }} />
              随机生成新 Key
            </Text>
            <Button
              type="primary"
              onClick={handleSaveApiKey}
              loading={saveMut.isPending}
              style={{ height: 42, paddingInline: 32, marginTop: 16 }}
            >
              保存 API Key
            </Button>
          </Card>
        </div>
      </div>
    </div>
  )
}
