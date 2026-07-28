import React from 'react';

interface Props {
  lines?: number;
  height?: string;
  className?: string;
}

export default function SkeletonLoader({ lines = 4, height = 'h-full', className = '' }: Props) {
  return (
    <div className={`bg-panel border border-border/50 rounded-lg p-4 flex flex-col gap-3 animate-pulse ${height} ${className}`}>
      <div className="h-4 bg-hover/80 rounded w-1/3 mb-2" />
      {Array.from({ length: lines }).map((_, i) => (
        <div key={i} className="flex gap-2 items-center">
          <div className="h-3 bg-hover/60 rounded flex-1" />
          <div className="h-3 bg-hover/40 rounded w-1/4" />
        </div>
      ))}
    </div>
  );
}
