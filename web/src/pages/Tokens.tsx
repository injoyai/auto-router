import { Card, Col, Row, Statistic, Spin, Table } from 'antd'
import { FireOutlined, ThunderboltOutlined, CheckCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { getStats } from '../api/logs'
import type { TokenStatRow } from '../api/logs'

const formatTokens = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`)

export default function Tokens() {
  const { data: stats, isLoading } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  })

  if (isLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  const tokensTotal = stats?.tokens?.total ?? 0
  const tokensPrompt = stats?.tokens?.prompt ?? 0
  const tokensCompletion = stats?.tokens?.completion ?? 0

  const modelRows = stats?.by_model ?? []
  const providerRows = stats?.by_provider ?? []

  const modelColumns = [
    { title: '模型名', dataIndex: 'model', key: 'model' },
    { title: '请求数', dataIndex: 'count', key: 'count', width: 100 },
    { title: '总Token', dataIndex: 'total_tokens', key: 'total_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '提示', dataIndex: 'prompt_tokens', key: 'prompt_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '补全', dataIndex: 'completion_tokens', key: 'completion_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '占比', key: 'percent', width: 80,
      render: (_: unknown, r: TokenStatRow) => tokensTotal > 0
        ? `${((r.total_tokens / tokensTotal) * 100).toFixed(1)}%`
        : '-' },
  ]

  const providerColumns = [
    { title: '服务商', dataIndex: 'provider', key: 'provider' },
    { title: '请求数', dataIndex: 'count', key: 'count', width: 100 },
    { title: '总Token', dataIndex: 'total_tokens', key: 'total_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '提示', dataIndex: 'prompt_tokens', key: 'prompt_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '补全', dataIndex: 'completion_tokens', key: 'completion_tokens', width: 120,
      render: (v: number) => v.toLocaleString() },
    { title: '占比', key: 'percent', width: 80,
      render: (_: unknown, r: TokenStatRow) => tokensTotal > 0
        ? `${((r.total_tokens / tokensTotal) * 100).toFixed(1)}%`
        : '-' },
  ]

  return (
    <div>
      <div className="page-title">Token 统计</div>
      <div className="page-subtitle">按模型和服务商维度查看 Token 消耗</div>

      <Row gutter={[20, 20]} style={{ marginBottom: 20 }}>
        <Col span={8}>
          <Card className="stat-card stat-card--indigo">
            <div className="stat-card-icon"><FireOutlined /></div>
            <Statistic title="总消耗" value={formatTokens(tokensTotal)} />
          </Card>
        </Col>
        <Col span={8}>
          <Card className="stat-card stat-card--mint">
            <div className="stat-card-icon"><ThunderboltOutlined /></div>
            <Statistic title="提示" value={formatTokens(tokensPrompt)} />
          </Card>
        </Col>
        <Col span={8}>
          <Card className="stat-card stat-card--violet">
            <div className="stat-card-icon"><CheckCircleOutlined /></div>
            <Statistic title="补全" value={formatTokens(tokensCompletion)} />
          </Card>
        </Col>
      </Row>

      <Card title="模型排行" className="aurora-card" style={{ marginBottom: 20 }}>
        <Table
          columns={modelColumns}
          dataSource={modelRows}
          rowKey="model"
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无数据' }}
        />
      </Card>

      <Card title="服务商排行" className="aurora-card">
        <Table
          columns={providerColumns}
          dataSource={providerRows}
          rowKey="provider"
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无数据' }}
        />
      </Card>
    </div>
  )
}
