import React from 'react';
import { Rss } from 'lucide-react';
export default function NewsFeed() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <Rss size={14} className='text-accent-amber' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Real-Time Macro Feed</span>
      </div>
      <div className='flex-1 overflow-auto flex flex-col'>
        {[
          { time: '10:42', text: 'Fed Chairman leaves rates unchanged, cites persistent inflation metrics.', impact: 'high' },
          { time: '10:15', text: 'ECB announces new bond purchasing parameters starting Q3.', impact: 'medium' },
          { time: '09:30', text: 'US Core CPI data matches consensus estimates at 3.2% YoY.', impact: 'medium' },
        ].map((news, i) => (
          <div key={i} className='p-3 border-b border-border/50 hover:bg-hover transition-colors'>
            <div className='flex justify-between text-[10px] mb-1'>
              <span className='font-mono text-accent-blue'>{news.time}</span>
              <span className={'uppercase font-bold ' + (news.impact === 'high' ? 'text-accent-red' : 'text-accent-amber')}>{news.impact} IMPACT</span>
            </div>
            <p className='text-xs text-white leading-relaxed'>{news.text}</p>
          </div>
        ))}
      </div>
    </div>
  );
}