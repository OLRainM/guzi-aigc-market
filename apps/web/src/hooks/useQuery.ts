import { useEffect, useState } from 'react';
import { request } from '../api';

export function useQuery<T>(path: string, enabled = true) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(enabled);

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError('');
    request<T>(path, { signal: controller.signal })
      .then(setData)
      .catch(reason => {
        if (reason instanceof DOMException && reason.name === 'AbortError') return;
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : '加载失败');
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, enabled]);

  return { data, error, loading };
}
