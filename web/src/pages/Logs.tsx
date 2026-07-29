import { useState } from 'react'
import { Table, Tag, Select, Input, Button, Space, Card } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { listLogs } from '../api/logs'
import type { RequestLog } from '../api/logs'

const reasonColors: Record<string, string> = {
  override: 'blue',
  session: 'green',
  judge: 'cyan',
  fallback: 'orange',
}

export default function Logs() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [reason, setReason] = useState<string | undefined>()
  const [model, setModel] = useState<string | undefined>()

  const [filters, setFilters] = useState({ reason: undefined as string | undefined, model: undefined as string | undefined })

  const { data, isLoading } = useQuery({
    queryKey: ['logs', page, pageSize, filters.reason, filters.model],
    queryFn: () => listLogs({ page, page_size: pageSize, reason: filters.reason, model: filters.model }),
  })

  const handleSearch = () => {
    setFilters({ reason, model })
    setPage(1)
  }

  const handleReset = () => {
    setReason(undefined)
    setModel(undefined)
    setFilters({ reason: undefined, model: undefined })
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
    {
      title: '路由原因', dataIndex: 'route_reason', key: 'route_reason', width: 100,
      render: (v: string) => <Tag color={reasonColors[v] ?? 'default'}>{v}</Tag>,
    },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 70,
      render: (v: number) => <span style={{ color: v < 400 ? '#52c41a' : '#ff4d4f' }}>{v}</span>,
    },
    {
      title: '延迟', dataIndex: 'latency_ms', key: 'latency_ms', width: 80,
      render: (v: number) => `${v}ms`,
    },
    {
      title: '错误', dataIndex: 'error', key: 'error', width: 200, ellipsis: true,
      render: (v: string) => v ? <span style={{ color: '#ff4d4f' }}>{v}</span> : '-',
    },
  ]

  return (
    <div>
      <Card size="small" style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            placeholder="路由原因"
            allowClear
            style={{ width: 140 }}
            value={reason}
            onChange={setReason}
            options={[
              { value: 'override', label: 'override' },
              { value: 'session', label: 'session' },
              { value: 'judge', label: 'judge' },
              { value: 'fallback', label: 'fallback' },
            ]}
          />
          <Input.Search
            placeholder="模型名"
            allowClear
            style={{ width: 200 }}
            value={model}
            onChange={(e) => setModel(e.target.value)}
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
          expandedRowRender: (r: RequestLog) => (
            r.judge_raw ? (
              <pre style={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: 13, background: '#fafafa', padding: 12, borderRadius: 6 }}>
                {r.judge_raw}
              </pre>
            ) : (
              <p style={{ color: '#999' }}>无判定原始数据</p>
            )
          ),
          rowExpandable: (r: RequestLog) => !!r.judge_raw,
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
