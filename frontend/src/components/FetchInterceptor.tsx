"use client";
import { useEffect } from 'react';

export default function FetchInterceptor() {
  useEffect(() => {
    if (typeof window !== 'undefined' && !(window as any)._fetchIntercepted) {
      const originalFetch = window.fetch;
      window.fetch = async function (...args) {
        const [resource, config] = args;
        return originalFetch(resource, config);
      };
      (window as any)._fetchIntercepted = true;
    }
  }, []);
  return null;
}
