import { Card, Col, Row, Select, Spin, Statistic, Button, Space, Radio } from 'antd'
import { FireOutlined, ThunderboltOutlined, CheckCircleOutlined, DatabaseOutlined, LineChartOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Column, ColumnConfig } from '@ant-design/charts'
import { getDailyStats, getDailyStatsByModel } from '../api/logs'
import { listProviders } from '../api/providers'
import { listModels } from '../api/models'

// 堆叠柱状图配色 - 植物系，随模型数量循环
const CHART_COLORS = [
  '#3a6b4d',
  '#5a9d6e',
  '#c87a4a',
  '#d4a04c',
  '#6a8d7a',
  '#b08968',
  '#4e7c5e',
  '#a89e85',
  '#8fae5a',
  '#d9b26a',
]

const formatTokens = (n: number) => {
  if (n < 1000) return `${n}`
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  return `${(n / 1_000_000_000).toFixed(1)}B`
}

const formatThousands = (n: number) => n.toLocaleString('en-US')

const formatDate = (d: Date) => {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

const modelKey = (model: string, provider: string) => (provider ? `${model} · ${provider}` : model)

type Metric = 'total_tokens' | 'prompt_tokens' | 'completion_tokens' | 'request_count' | 'cache_tokens'

const metricLabels: Record<Metric, string> = {
  total_tokens: '总 Token',
  prompt_tokens: '输入 Token',
  completion_tokens: '输出 Token',
  request_count: '请求数',
  cache_tokens: '缓存命中',
}

export default function UsageTrend() {
  const [provider, setProvider] = useState<string | undefined>()
  const [modelId, setModelId] = useState<number | undefined>()
  const [days, setDays] = useState(30)
  const [metric, setMetric] = useState<Metric>('total_tokens')
  const [searchNonce, setSearchNonce] = useState(0)

  const { data: providers } = useQuery({ queryKey: ['providers'], queryFn: listProviders })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: listModels })

  const modelName = modelId != null ? (models ?? []).find(m => m.id === modelId)?.name : undefined

  const { data: rows, isLoading } = useQuery({
    queryKey: ['daily-stats', provider, modelName, days, searchNonce],
    queryFn: () => getDailyStats({ provider, model: modelName, days }),
  })

  const { data: byModelRows, isLoading: byModelLoading } = useQuery({
    queryKey: ['daily-stats-by-model', provider, modelName, days, searchNonce],
    queryFn: () => getDailyStatsByModel({ provider, model: modelName, days }),
  })

  const handleSearch = () => setSearchNonce(n => n + 1)
  const handleReset = () => {
    setProvider(undefined)
    setModelId(undefined)
    setDays(30)
    setSearchNonce(n => n + 1)
  }

  if (isLoading || byModelLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  const data = rows ?? []
  const totalTokens = data.reduce((s, r) => s + r.total_tokens, 0)
  const totalPrompt = data.reduce((s, r) => s + r.prompt_tokens, 0)
  const totalCompletion = data.reduce((s, r) => s + r.completion_tokens, 0)
  const totalCache = data.reduce((s, r) => s + r.cache_tokens, 0)
  const totalRequests = data.reduce((s, r) => s + r.request_count, 0)

  // 近 days 天的完整日期序列（升序），无数据的天也占据 x 轴位置
  const dayList: string[] = []
  const today = new Date()
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today)
    d.setDate(today.getDate() - i)
    dayList.push(formatDate(d))
  }

  // 提取所有出现过的系列（model+provider），并为每个日期×系列补齐 0 值点，
  // 使 x 轴始终固定为完整日期序列，图例筛选某个系列后空日期也不会消失
  const keyInfo = new Map<string, { model: string; provider: string }>()
  const byDateKey = new Map<string, Map<string, number>>()
  for (const r of byModelRows ?? []) {
    const key = modelKey(r.model || '未知', r.provider)
    if (!keyInfo.has(key)) keyInfo.set(key, { model: r.model || '未知', provider: r.provider })
    let m = byDateKey.get(r.date)
    if (!m) { m = new Map(); byDateKey.set(r.date, m) }
    m.set(key, r[metric])
  }
  const allKeys = Array.from(keyInfo.keys())

  const chartData = (byModelRows ?? []).length === 0
    ? []
    : dayList.flatMap((date) => {
        const keyMap = byDateKey.get(date)
        return allKeys.map((key) => {
          const info = keyInfo.get(key)!
          return {
            date,
            key,
            model: info.model,
            provider: info.provider,
            value: keyMap?.get(key) ?? 0,
          }
        })
      })

  const columnConfig: Omit<ColumnConfig, 'data'> = {
    xField: 'date',
    yField: 'value',
    seriesField: 'key',
    isStack: true,
    color: CHART_COLORS,
    columnStyle: { maxWidth: 42 },
    legend: {
      position: 'top',
      marker: { symbol: 'circle', style: { r: 5 } },
      itemName: { style: { fontFamily: "'DM Sans', sans-serif", fontSize: 12, fill: '#6a604c' } },
    },
    tooltip: {
      customContent: (title, items) => {
        if (!items || items.length === 0) {
          return `<div style="font-weight:700;color:#1f1a12;">${title}</div>`
        }
        // 当天所有系列的汇总用量
        const dayTotal = items.reduce((s: number, it: any) => s + (it.data?.value ?? 0), 0)
        const body = items.map((it: any) => {
          const d = it.data ?? {}
          const label = d.provider ? `${d.model} · ${d.provider}` : (d.model || '未知')
          const color = it.color || CHART_COLORS[0]
          return `
            <div style="display:flex;align-items:center;gap:8px;padding:2px 0;min-width:200px;">
              <span style="width:8px;height:8px;border-radius:50%;background:${color};flex-shrink:0;"></span>
              <span style="color:#6a604c;">${label}</span>
              <span style="margin-left:auto;font-weight:700;color:#1f1a12;font-family:'Bricolage Grotesque',sans-serif;">${formatThousands(d.value ?? 0)}</span>
            </div>`
        }).join('')
        return `<div style="font-weight:700;color:#1f1a12;margin-bottom:4px;">${title}</div>${body}<div style="display:flex;align-items:center;gap:8px;padding:4px 0 0;min-width:200px;border-top:1px dashed rgba(106,96,76,0.25);margin-top:4px;"><span style="color:#3a6b4d;font-weight:700;">汇总</span><span style="margin-left:auto;font-weight:700;color:#3a6b4d;font-family:'Bricolage Grotesque',sans-serif;">${formatThousands(dayTotal)}</span></div>`
      },
      domStyles: {
        'g2-tooltip': {
          fontFamily: "'DM Sans', sans-serif",
          fontSize: '12px',
          borderRadius: '12px',
          boxShadow: '0 10px 30px rgba(80, 60, 30, 0.18)',
          background: 'rgba(255, 255, 255, 0.92)',
          backdropFilter: 'blur(18px)',
          border: '1px solid rgba(255, 255, 255, 0.7)',
          padding: '10px 14px',
        },
      },
    },
    xAxis: {
      label: {
        style: { fontFamily: "'DM Sans', sans-serif", fontSize: 11, fill: '#a89e85' },
        autoRotate: true,
        autoHide: true,
      },
    },
    yAxis: {
      label: {
        style: { fontFamily: "'DM Sans', sans-serif", fontSize: 11, fill: '#a89e85' },
        formatter: (v: string) => formatTokens(Number(v)),
      },
    },
    animation: { appear: { animation: 'wave-in', duration: 600 } },
    interactions: [{ type: 'element-active' }],
  }

  const providerOptions = (providers ?? []).map(p => ({ value: p.name, label: p.name }))
  const selectedProvider = (providers ?? []).find(p => p.name === provider)
  const modelOptions = (models ?? [])
    .filter(m => !selectedProvider || m.provider_id === selectedProvider.id)
    .map(m => {
      const pName = (providers ?? []).find(p => p.id === m.provider_id)?.name
      const label = selectedProvider ? m.name : (pName ? `${m.name} (${pName})` : m.name)
      return { value: m.id, label }
    })

  return (
    <div>
      <div className="page-title">用量趋势</div>
      <div className="page-subtitle">按天查看 Token 消耗和请求量变化</div>

      <Card size="small" className="filter-card" style={{ marginBottom: 16 }}>
        <Space wrap size="middle">
          <Select
            placeholder="服务商"
            allowClear
            style={{ width: 160 }}
            value={provider}
            onChange={(v) => { setProvider(v); setModelId(undefined) }}
            options={providerOptions}
          />
          <Select
            placeholder="模型"
            allowClear
            showSearch
            optionFilterProp="label"
            style={{ width: 240 }}
            value={modelId}
            onChange={setModelId}
            options={modelOptions}
          />
          <Radio.Group value={days} onChange={(e) => setDays(e.target.value)}>
            <Radio.Button value={7}>7天</Radio.Button>
            <Radio.Button value={14}>14天</Radio.Button>
            <Radio.Button value={30}>30天</Radio.Button>
            <Radio.Button value={90}>90天</Radio.Button>
          </Radio.Group>
          <Button type="primary" onClick={handleSearch}>查询</Button>
          <Button onClick={handleReset}>重置</Button>
        </Space>
      </Card>

      <Row gutter={[20, 20]} style={{ marginBottom: 20 }}>
        <Col span={5} className="aurora-fade-in aurora-fade-in-1">
          <Card className="stat-card stat-card--indigo">
            <div className="stat-card-icon"><FireOutlined /></div>
            <Statistic title="总消耗" value={formatTokens(totalTokens)} />
          </Card>
        </Col>
        <Col span={5} className="aurora-fade-in aurora-fade-in-2">
          <Card className="stat-card stat-card--mint">
            <div className="stat-card-icon"><ThunderboltOutlined /></div>
            <Statistic title="输入" value={formatTokens(totalPrompt)} />
          </Card>
        </Col>
        <Col span={5} className="aurora-fade-in aurora-fade-in-3">
          <Card className="stat-card stat-card--violet">
            <div className="stat-card-icon"><CheckCircleOutlined /></div>
            <Statistic title="输出" value={formatTokens(totalCompletion)} />
          </Card>
        </Col>
        <Col span={5} className="aurora-fade-in aurora-fade-in-4">
          <Card className="stat-card stat-card--amber">
            <div className="stat-card-icon"><DatabaseOutlined /></div>
            <Statistic title="缓存命中" value={formatTokens(totalCache)} />
          </Card>
        </Col>
        <Col span={4} className="aurora-fade-in aurora-fade-in-4">
          <Card className="stat-card stat-card--indigo">
            <div className="stat-card-icon"><LineChartOutlined /></div>
            <Statistic title="请求数" value={totalRequests} />
          </Card>
        </Col>
      </Row>

      <Card title="每日用量趋势" className="aurora-card aurora-chart-card usage-trend-card">
        <Space style={{ marginBottom: 16 }}>
          <span style={{ fontSize: 13, color: '#8a7f66' }}>指标：</span>
          <Radio.Group value={metric} onChange={(e) => setMetric(e.target.value)} size="small">
            {(Object.keys(metricLabels) as Metric[]).map(k => (
              <Radio.Button key={k} value={k}>{metricLabels[k]}</Radio.Button>
            ))}
          </Radio.Group>
        </Space>
        {chartData.length > 0 ? (
          <Column {...columnConfig} data={chartData} style={{ height: 400 }} />
        ) : (
          <p className="aurora-chart-empty">暂无数据</p>
        )}
      </Card>
    </div>
  )
}
