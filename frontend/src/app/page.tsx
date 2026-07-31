import React from 'react';
import ErrorBoundary from '../components/ErrorBoundary';
import TerminalClient from '../components/TerminalClient';

export default function Page() {
  return (
    <ErrorBoundary title="Fatal App Error">
      <TerminalClient />
    </ErrorBoundary>
  );
}
