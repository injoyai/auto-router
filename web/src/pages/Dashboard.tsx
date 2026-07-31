import { Card, Col, Row, Statistic, Spin } from 'antd'
import { Pie, PieConfig } from '@ant-design/charts'
import {
  ThunderboltOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  FireOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { getStats, listLogs } from '../api/logs'

// 高级饼图配色 - 植物系深浅渐变 + 暖铜点缀
const PIE_COLORS = [
  '#3a6b4d',
  '#5a9d6e',
  '#c87a4a',
  '#d4a04c',
  '#a89e85',
  '#6a8d7a',
  '#b08968',
  '#4e7c5e',
]

/**
 * 构建高级环形饼图配置
 * - 内环 donut 形态，中心指标卡
 * - 悬浮高亮 + 其余淡出
 * - 悬浮时中心文本联动
 * - 柔和阴影 + 玻璃描边 + 入场动画
 */
function buildPieConfig(opts: {
  totalValue: number
  titleText: string
  formatValue?: (v: number) => string
}): Omit<PieConfig, 'data'> {
  const { totalValue, titleText, formatValue = (v) => `${v}` } = opts

  return {
    angleField: 'value',
    colorField: 'type',
    radius: 0.85,
    innerRadius: 0.62,
    color: PIE_COLORS,
    // 环形图样式：玻璃描边 + 柔和阴影 + 轻透
    pieStyle: {
      stroke: 'rgba(255, 255, 255, 0.9)',
      lineWidth: 2,
      fillOpacity: 0.92,
      shadowColor: 'rgba(58, 107, 77, 0.18)',
      shadowBlur: 14,
      shadowOffsetX: 0,
      shadowOffsetY: 8,
    },
    // 状态：悬浮高亮 / 其余淡出
    state: {
      active: {
        style: {
          lineWidth: 3,
          fillOpacity: 1,
          stroke: 'rgba(255, 255, 255, 1)',
          shadowColor: 'rgba(58, 107, 77, 0.38)',
          shadowBlur: 28,
          shadowOffsetY: 10,
        },
      },
      inactive: {
        style: {
          fillOpacity: 0.32,
          stroke: 'rgba(255, 255, 255, 0.35)',
          lineWidth: 1,
        },
      },
    },
    // 中心指标卡：默认显示总计，悬浮时联动显示当前扇区
    statistic: {
      title: {
        content: titleText,
        style: {
          fontFamily: "'DM Sans', sans-serif",
          fontSize: '12px',
          fontWeight: 500,
          color: '#a89e85',
          textTransform: 'uppercase' as const,
          letterSpacing: '0.1em',
          lineHeight: 1,
        },
        formatter: (datum) => (datum ? datum.type : titleText),
      },
      content: {
        content: formatValue(totalValue),
        style: {
          fontFamily: "'Bricolage Grotesque', sans-serif",
          fontSize: '30px',
          fontWeight: 700,
          color: '#1f1a12',
          lineHeight: 1,
          letterSpacing: '-0.02em',
        },
        formatter: (datum) =>
          datum ? formatValue(datum.value) : formatValue(totalValue),
      },
    },
    // 蜘蛛标签：名称 + 百分比
    label: {
      type: 'spider',
      content: '{name}  {percentage}',
      style: {
        fontFamily: "'DM Sans', sans-serif",
        fontSize: 12,
        fill: '#6a604c',
        fontWeight: 500,
      },
      // 标签引导线样式
      labelLine: {
        style: {
          stroke: '#ccc3ae',
          lineWidth: 1,
        },
      },
    },
    legend: {
      position: 'bottom',
      radio: {},
      itemSpacing: 16,
      marker: {
        symbol: 'circle',
        style: {
          r: 5,
        },
      },
      itemName: {
        style: {
          fontFamily: "'DM Sans', sans-serif",
          fontSize: 12,
          fill: '#6a604c',
        },
      },
    },
    tooltip: {
      showTitle: false,
      showMarkers: false,
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
        'g2-tooltip-name': {
          color: '#6a604c',
          fontWeight: 500,
        },
        'g2-tooltip-value': {
          color: '#1f1a12',
          fontWeight: 700,
          fontFamily: "'Bricolage Grotesque', sans-serif",
        },
      },
    },
    // 交互：悬浮联动中心文本 + 图例联动 + 元素高亮
    interactions: [
      { type: 'pie-statistic-active' },
      { type: 'legend-active' },
      { type: 'element-active' },
    ],
    // 入场动画：扇形扫描 + 缓动
    animation: {
      appear: {
        animation: 'wave-in',
        duration: 1000,
        easing: 'easeCubicOut',
      },
    },
  }
}

export default function Dashboard() {
  const { data: stats, isLoading: statsLoading, isError: statsError } = useQuery({
    queryKey: ['stats'],
    queryFn: getStats,
  })

  const { data: logsData, isLoading: logsLoading, isError: logsError } = useQuery({
    queryKey: ['logs', 'dashboard'],
    queryFn: () => listLogs({ page: 1, page_size: 200 }),
  })

  if (statsLoading || logsLoading) {
    return <Spin size="large" style={{ display: 'block', marginTop: 100 }} />
  }

  if (statsError || logsError) {
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

  const tokensTotal = stats?.tokens?.total ?? 0
  const formatTokens = (n: number) => {
    if (n < 1000) return `${n}`
    if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
    if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    return `${(n / 1_000_000_000).toFixed(1)}B`
  }

  const reasonLabels: Record<string, string> = {
    override: '指定路由',
    judge: '智能路由',
    judge_call: '判定调用',
    fallback: '兜底路由',
    test: '测试',
  }
  const pieData = (stats?.by_reason ?? []).map((r) => ({
    type: reasonLabels[r.Reason] ?? r.Reason,
    value: r.Count,
  }))

  const tokenPieData = (stats?.by_model ?? []).map((r) => ({
    type: r.model,
    value: r.total_tokens,
  }))

  const pieConfig = buildPieConfig({
    totalValue: totalCount,
    titleText: '总请求',
    formatValue: (v) => `${v}`,
  })

  const tokenPieConfig = buildPieConfig({
    totalValue: tokensTotal,
    titleText: '总 Token',
    formatValue: formatTokens,
  })

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
            <div className="stat-card-icon"><FireOutlined /></div>
            <Statistic title="Token 消耗" value={formatTokens(tokensTotal)} />
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
          <Card title="路由类型分布" className="aurora-card aurora-chart-card">
            {pieData.length > 0 ? (
              <Pie {...pieConfig} data={pieData} style={{ height: 340 }} />
            ) : (
              <p className="aurora-chart-empty">暂无数据</p>
            )}
          </Card>
        </Col>
        <Col span={12} className="aurora-fade-in aurora-fade-in-4">
          <Card title="Token 按模型分布" className="aurora-card aurora-chart-card">
            {tokenPieData.length > 0 ? (
              <Pie {...tokenPieConfig} data={tokenPieData} style={{ height: 340 }} />
            ) : (
              <p className="aurora-chart-empty">暂无数据</p>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
