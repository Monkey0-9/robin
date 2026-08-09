"use client";
import { useEffect } from 'react';

export default function FetchInterceptor() {
  useEffect(() => {
    // Interceptor disabled: wrapping window.fetch in an async function
    // breaks Next.js dev overlay's promise tracking for network errors.
    // Auth 401s should be handled in the API utility or store directly.
  }, []);
  return null;
}
