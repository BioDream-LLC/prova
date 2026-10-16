import { useEffect, useRef } from 'react'
import * as echarts from 'echarts'
import { AreaSeries, createChart, type UTCTimestamp } from 'lightweight-charts'

/** EChart renders an Apache ECharts option into a resizing canvas. */
export function EChart({ option, height = 260 }: { option: echarts.EChartsOption; height?: number }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const chart = echarts.init(el, undefined, { renderer: 'canvas' })
    chart.setOption(option)
    const ro = new ResizeObserver(() => chart.resize())
    ro.observe(el)
    return () => {
      ro.disconnect()
      chart.dispose()
    }
  }, [option])
  return <div ref={ref} style={{ height, width: '100%' }} />
}

/** Area is a TradingView lightweight-charts area series — the finance-grade time-series look. */
export function Area({ data, height = 220, color = '#6366f1' }: {
  data: { time: number; value: number }[]
  height?: number
  color?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const chart = createChart(el, {
      height,
      layout: { background: { color: 'transparent' }, textColor: '#93a0b8', fontFamily: 'Inter, sans-serif', attributionLogo: false },
      grid: { vertLines: { color: 'rgba(31,42,68,0.5)' }, horzLines: { color: 'rgba(31,42,68,0.5)' } },
      rightPriceScale: { borderColor: '#1f2a44' },
      timeScale: { borderColor: '#1f2a44', timeVisible: true, secondsVisible: false },
      crosshair: { mode: 0 },
    })
    const series = chart.addSeries(AreaSeries, {
      lineColor: color,
      topColor: color + '55',
      bottomColor: color + '05',
      lineWidth: 2,
      priceLineVisible: false,
    })
    series.setData(data.map((d) => ({ time: d.time as UTCTimestamp, value: d.value })))
    chart.timeScale().fitContent()
    const ro = new ResizeObserver(() => chart.applyOptions({ width: el.clientWidth }))
    ro.observe(el)
    return () => {
      ro.disconnect()
      chart.remove()
    }
  }, [data, height, color])
  return <div ref={ref} style={{ width: '100%' }} />
}
