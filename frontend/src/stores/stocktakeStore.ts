import { useCallback, useState } from 'react'
import { stocktakeAPI, type PageParams } from '../api'
import type { PageResult, StocktakeItem, StocktakeResult, StocktakeTask } from '../types/domain'

export function useStocktakeStore() {
  const [data, setData] = useState<PageResult<StocktakeTask>>({ items: [], total: 0, page: 1, pageSize: 10 })
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (params: PageParams & { state?: string; containerId?: number } = {}) => {
    setLoading(true)
    try { setData(await stocktakeAPI.list(params)) } finally { setLoading(false) }
  }, [])
  return { data, loading, load }
}

export function useStocktakeItemStore(taskId: number) {
  const [data, setData] = useState<PageResult<StocktakeItem>>({ items: [], total: 0, page: 1, pageSize: 50 })
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (params: PageParams & { result?: StocktakeResult; pending?: boolean } = {}) => {
    setLoading(true)
    try { setData(await stocktakeAPI.listItems(taskId, params)) } finally { setLoading(false) }
  }, [taskId])
  return { data, loading, load }
}
