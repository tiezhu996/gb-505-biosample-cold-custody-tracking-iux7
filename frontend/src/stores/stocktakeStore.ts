import { useCallback, useState } from 'react'
import { stocktakeAPI, type PageParams } from '../api'
import type { PageResult, Stocktake } from '../types/domain'

export function useStocktakeStore() {
  const [data, setData] = useState<PageResult<Stocktake>>({ items: [], total: 0, page: 1, pageSize: 10 })
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (params: PageParams & { state?: string; containerId?: number } = {}) => {
    setLoading(true)
    try { setData(await stocktakeAPI.list(params)) } finally { setLoading(false) }
  }, [])
  return { data, loading, load }
}
