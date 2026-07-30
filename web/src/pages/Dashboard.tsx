import { Card, Col, Row, Statistic, Spin } from 'antd'
import { Pie, Column } from '@ant-design/charts'
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { getStats, listLogs } from '../api/logs'
import { listModels } from '../api/models'

export default function Dashboard() {
  const { data: stats, isLoading: statsLoading, isError: statsError } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  })

  const { data: models, isLoading: modelsLoading, isError: modelsError } = useQuery({
    queryKey: ['models'],
    queryFn: listModels,
  })

  const { data: logsData, isLoading: logsLoading, isError: logsError } = useQuery({
    queryKey: ['logs', 'dashboard'],
    queryFn: () => listLogs({ page: 1, page_size: 200 }),
  })

  if (statsLoading || logsLoading || modelsLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  if (statsError || logsError || modelsError) {
    return (
      <Card className="aurora-card">
        <p style={{ color: '#d05a4a', textAlign: 'center', padding: 40 }}>
          数据加载失败，请稍后重试
        </p>
      </Card>
    )
  }

  const logs = logsData?.data ?? []
  const totalCount = stats?.total ?? 0
  const successCount = logs.filter((l) => l.status < 400).length
  const successRate = logs.length > 0 ? ((successCount / logs.length) * 100).toFixed(1) : '0.0'

  const avgLatency = logs.length > 0
    ? Math.round(logs.reduce((sum, l) => sum + l.latency_ms, 0) / logs.length)
    : 0

  const activeModelCount = models?.filter((m) => m.enabled).length ?? 0

  const pieData = (stats?.by_reason ?? []).map((r) => ({
    type: r.Reason,
    value: r.Count,
  }))

  const modelMap: Record<string, number> = {}
  logs.forEach((l) => {
    if (l.routed_model) {
      modelMap[l.routed_model] = (modelMap[l.routed_model] ?? 0) + 1
    }
  })
  const columnData = Object.entries(modelMap)
    .map(([model, count]) => ({ model, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 10)

  const pieConfig = {
    data: pieData,
    angleField: 'value',
    colorField: 'type',
    radius: 0.8,
    label: { type: 'outer' as const },
    legend: { position: 'bottom' as const },
    color: ['#3a6b4d', '#c87a4a', '#d4a04c', '#5a9d6e', '#a89e85'],
  }

  const columnConfig = {
    data: columnData,
    xField: 'model',
    yField: 'count',
    label: { position: 'top' as const },
    xAxis: { label: { autoRotate: true, autoHide: false } },
    color: '#3a6b4d',
    columnStyle: { radius: [6, 6, 0, 0] },
  }

  return (
    <div>
      <div className="page-title">仪表盘</div>
      <div className="page-subtitle">实时监控你的 AI 网关运行状态</div>
      <Row gutter={[20, 20]} style={{ marginBottom: 20 }}>
        <Col span={6} className="aurora-fade-in aurora-fade-in-1">
          <Card className="stat-card stat-card--indigo">
            <div className="stat-card-icon"><ThunderboltOutlined /></div>
            <Statistic title="总请求数" value={totalCount} />
          </Card>
        </Col>
        <Col span={6} className="aurora-fade-in aurora-fade-in-2">
          <Card className="stat-card stat-card--mint">
            <div className="stat-card-icon"><CheckCircleOutlined /></div>
            <Statistic title="成功率" value={successRate} suffix="%" />
          </Card>
        </Col>
        <Col span={6} className="aurora-fade-in aurora-fade-in-3">
          <Card className="stat-card stat-card--violet">
            <div className="stat-card-icon"><AppstoreOutlined /></div>
            <Statistic title="活跃模型数" value={activeModelCount} />
          </Card>
        </Col>
        <Col span={6} className="aurora-fade-in aurora-fade-in-4">
          <Card className="stat-card stat-card--amber">
            <div className="stat-card-icon"><ClockCircleOutlined /></div>
            <Statistic title="平均延迟" value={avgLatency} suffix="ms" />
          </Card>
        </Col>
      </Row>
      <Row gutter={20}>
        <Col span={12} className="aurora-fade-in aurora-fade-in-3">
          <Card title="路由原因分布" className="aurora-card">
            {pieData.length > 0 ? (
              <Pie {...pieConfig} />
            ) : (
              <p style={{ color: '#a89e85', textAlign: 'center', padding: 40 }}>暂无数据</p>
            )}
          </Card>
        </Col>
        <Col span={12} className="aurora-fade-in aurora-fade-in-4">
          <Card title="模型使用占比" className="aurora-card">
            {columnData.length > 0 ? (
              <Column {...columnConfig} />
            ) : (
              <p style={{ color: '#a89e85', textAlign: 'center', padding: 40 }}>暂无数据</p>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
