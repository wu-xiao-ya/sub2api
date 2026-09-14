import { describe, expect, it } from 'vitest'
import { usageLatencyStages, latencyTimingOrigin, latencyCSVValues } from '../usageLatencyStages'

describe('measured latency stages', () => {
  it('never invents historical milestones', () => {
    expect(usageLatencyStages(null).map(s => s.value)).toEqual([null,null,null,null,null])
    expect(latencyCSVValues(null)).toEqual(['','','','','',''])
    expect(latencyTimingOrigin(null)).toBe('latencyOriginUnknown')
  })
  it('compares successive available stages and retains zero', () => {
    const stages = usageLatencyStages({version:2,first_response_ms:0,first_output_ms:20,total_duration_ms:80})
    expect(stages.map(s => s.delta)).toEqual([null,null,20,null,60])
    expect(stages[2].fromLabel).toBe('latencyFirstResponse')
  })

  it('preserves the directly measured legacy total, never a V2 fallback', () => {
    expect(usageLatencyStages(null, 1500).map(s => s.value)).toEqual([null,null,null,null,1500])
    expect(usageLatencyStages({version:2,first_event_ms:300},1500)[4].value).toBeNull()
    expect(latencyTimingOrigin(null,1500)).toBe('latencyOriginForward')
    expect(latencyCSVValues(null,1500)).toEqual([1,'','','','',1500])
  })
  it('separates timing origins and rejects invalid differences', () => {
    expect(latencyTimingOrigin({first_event_ms:2})).toBe('latencyOriginForward')
    expect(latencyTimingOrigin({version:2,first_event_ms:2})).toBe('latencyOriginIngress')
    expect(usageLatencyStages({first_response_ms:5,first_event_ms:2})[1].delta).toBeNull()
    expect(usageLatencyStages({first_response_ms:-1,first_event_ms:NaN})[0].value).toBeNull()
  })
})
