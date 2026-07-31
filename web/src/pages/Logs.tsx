import { useState } from 'react'
import { Table, Tag, Select, Input, Button, Space, Card } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { listLogs } from '../api/logs'
import type { RequestLog } from '../api/logs'

const reasonColors: Record<string, string> = {
  override: 'blue',
  judge: 'geekblue',
  judge_call: 'cyan',
  fallback: 'orange',
  test: 'purple',
}

// 格式化耗时:ms -> ms/s/m,保留 2 位有效小数
const formatLatency = (ms: number) => {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60_000).toFixed(2)}m`
}

// 后端 route_reason 为英文,界面展示成中文(value 仍用英文传给后端过滤)
const reasonLabels: Record<string, string> = {
  override: '指定路由',
  judge: '智能路由',
  judge_call: '判定调用',
  fallback: '兜底路由',
  test: '测试',
}

export default function Logs() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [reason, setReason] = useState<string | undefined>()
  const [model, setModel] = useState<string | undefined>()

  const [filters, setFilters] = useState({ reason: undefined as string | undefined, model: undefined as string | undefined })
  // searchNonce forces a refetch on every "查询" click even when filters are
  // unchanged (React Query skips re-fetching when the queryKey is identical).
  const [searchNonce, setSearchNonce] = useState(0)

  const { data, isLoading } = useQuery({
    queryKey: ['logs', page, pageSize, filters.reason, filters.model, searchNonce],
    queryFn: () => listLogs({ page, page_size: pageSize, reason: filters.reason, model: filters.model }),
  })

  const handleSearch = () => {
    setFilters({ reason, model })
    setSearchNonce((n) => n + 1)
    setPage(1)
  }

  const handleReset = () => {
    setReason(undefined)
    setModel(undefined)
    setFilters({ reason: undefined, model: undefined })
    setSearchNonce((n) => n + 1)
    setPage(1)
  }

  const columns = [
    {
      title: '时间', dataIndex: 'created_at', key: 'created_at', width: 170,
      render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '-',
    },
    {
      title: '会话ID', dataIndex: 'session_id', key: 'session_id', width: 140,
      render: (v: string) => v?.slice(0, 12) ?? '-',
    },
    { title: '请求模型', dataIndex: 'requested_model', key: 'requested_model', width: 120 },
    { title: '路由模型', dataIndex: 'routed_model', key: 'routed_model', width: 120 },
    { title: '服务商', dataIndex: 'provider_name', key: 'provider_name', width: 100, render: (v: string) => v || '-' },
    {
      title: '路由类型', dataIndex: 'route_reason', key: 'route_reason', width: 100,
      render: (v: string) => <Tag color={reasonColors[v] ?? 'default'}>{reasonLabels[v] ?? v}</Tag>,
    },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 70,
      render: (v: number) => <span style={{ color: v < 400 ? '#10b981' : '#f43f5e', fontWeight: 600 }}>{v}</span>,
    },
    {
      title: '耗时', dataIndex: 'latency_ms', key: 'latency_ms', width: 80,
      render: (v: number) => formatLatency(v),
    },
    {
      title: '重试', dataIndex: 'retry_count', key: 'retry_count', width: 60,
      render: (v: number) => v > 0 ? <Tag color="orange">{v}</Tag> : '-',
    },
    {
      title: 'Tokens', dataIndex: 'total_tokens', key: 'total_tokens', width: 90,
      render: (v: number) => v > 0 ? v : '-',
    },
    {
      title: '错误', dataIndex: 'error', key: 'error', width: 200, ellipsis: true,
      render: (v: string) => v ? <span style={{ color: '#f43f5e' }}>{v}</span> : '-',
    },
  ]

  return (
    <div>
      <div className="page-title">请求日志</div>
      <div className="page-subtitle">查看所有请求的路由记录与详细信息</div>
      <Card size="small" className="filter-card" style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            placeholder="路由类型"
            allowClear
            style={{ width: 140 }}
            value={reason}
            onChange={setReason}
            options={[
              { value: 'override', label: '指定路由' },
              { value: 'judge', label: '智能路由' },
              { value: 'fallback', label: '兜底路由' },
              { value: 'test', label: '测试' },
            ]}
          />
          <Input.Search
            placeholder="模型名"
            allowClear
            style={{ width: 200 }}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            onSearch={handleSearch}
          />
          <Button type="primary" onClick={handleSearch}>查询</Button>
          <Button onClick={handleReset}>重置</Button>
        </Space>
      </Card>

      <Table
        columns={columns}
        dataSource={data?.data}
        rowKey="id"
        loading={isLoading}
        scroll={{ x: 1200 }}
        expandable={{
          expandedRowRender: (r: RequestLog) => {
            const hasJudge = !!r.judge_model
            const hasRaw = !!r.judge_raw
            if (!hasJudge && !hasRaw) {
              return <p style={{ color: '#9fa1b5' }}>无判定数据</p>
            }
            return (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {hasJudge && (
                  <div style={{ display: 'flex', gap: 28, flexWrap: 'wrap', fontSize: 13, color: '#6a604c' }}>
                    <span><strong style={{ color: '#4e4636' }}>判定模型:</strong> {r.judge_model}</span>
                    <span><strong style={{ color: '#4e4636' }}>判定耗时:</strong> {formatLatency(r.judge_latency_ms)}</span>
                    <span>
                      <strong style={{ color: '#4e4636' }}>判定 Token:</strong>
                      {' '}提示 {r.judge_prompt_tokens} / 补全 {r.judge_completion_tokens} / 合计 {r.judge_total_tokens}
                    </span>
                  </div>
                )}
                {hasRaw && (
                  <pre style={{ whiteSpace: 'pre-wrap', fontFamily: "'JetBrains Mono', monospace", fontSize: 13, background: '#f8f9fc', padding: 16, borderRadius: 12, border: '1px solid #f1f2f8', margin: 0 }}>
                    {r.judge_raw}
                  </pre>
                )}
              </div>
            )
          },
          rowExpandable: (r: RequestLog) => !!r.judge_raw || !!r.judge_model,
        }}
        pagination={{
          current: page,
          pageSize,
          total: data?.total ?? 0,
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条`,
          onChange: (p, ps) => { setPage(p); setPageSize(ps) },
        }}
      />
    </div>
  )
}
