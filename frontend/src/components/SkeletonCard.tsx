import React from 'react';

interface SkeletonCardProps {
  className?: string;
  lines?: number;
  height?: string;
}

/** Animated skeleton placeholder for loading states. */
export function SkeletonCard({ className = '', lines = 3, height = 'h-4' }: SkeletonCardProps) {
  return (
    <div className={`animate-pulse space-y-2 ${className}`}>
      {Array.from({ length: lines }).map((_, i) => (
        <div
          key={i}
          className={`bg-border/40 rounded ${height}`}
          style={{ width: i === lines - 1 ? '60%' : '100%' }}
        />
      ))}
    </div>
  );
}

interface SkeletonRowProps {
  cols?: number;
  className?: string;
}

export function SkeletonRow({ cols = 4, className = '' }: SkeletonRowProps) {
  return (
    <div className={`flex gap-2 animate-pulse ${className}`}>
      {Array.from({ length: cols }).map((_, i) => (
        <div key={i} className="h-3 bg-border/40 rounded flex-1" />
      ))}
    </div>
  );
}

interface SkeletonTableProps {
  rows?: number;
  cols?: number;
}

export function SkeletonTable({ rows = 5, cols = 4 }: SkeletonTableProps) {
  return (
    <div className="space-y-1.5 p-2">
      {Array.from({ length: rows }).map((_, i) => (
        <SkeletonRow key={i} cols={cols} />
      ))}
    </div>
  );
}

/** Small inline spinner for button loading states. */
export function Spinner({ className = '' }: { className?: string }) {
  return (
    <span
      className={`inline-block w-3 h-3 border-2 border-current/30 border-t-current rounded-full animate-spin ${className}`}
    />
  );
}

export default SkeletonCard;
