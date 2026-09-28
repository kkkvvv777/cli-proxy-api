import { useCallback, useEffect, useRef } from 'react';
import { useAuthStore } from '@/stores';

// Discard results from a previous connection, logout or an unmounted route.
export function useCompanySession() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const key = useAuthStore((state) => state.managementKey);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return useCallback(() => {
    const current = useAuthStore.getState();
    return (
      mounted.current &&
      current.isAuthenticated &&
      current.apiBase === apiBase &&
      current.managementKey === key
    );
  }, [apiBase, key]);
}
