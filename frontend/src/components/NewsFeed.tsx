import React, { useState, useEffect } from 'react';
import { Rss, Loader2 } from 'lucide-react';
import { useAuthStore } from '../store/useAuthStore';

interface NewsItem {
  time: string;
  text: string;
  impact: string;
}

export default function NewsFeed() {
  const [newsList, setNewsList] = useState<NewsItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchNews = async () => {
      try {
        const token = useAuthStore.getState().getToken() || '';
        const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';
        const res = await fetch(`${GATEWAY_URL}/api/ai/macro_feed`, {
          headers: token ? { Authorization: `Bearer ${token}` } : {}
        });
        if (res.ok) {
          const data = await res.json();
          setNewsList(data);
        }
      } catch (err) {
        console.error("Failed to fetch macro news:", err);
      } finally {
        setLoading(false);
      }
    };

    fetchNews();
    const interval = setInterval(fetchNews, 30000); // Poll every 30 seconds
    return () => clearInterval(interval);
  }, []);

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg font-sans'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          <Rss size={14} className='text-accent-amber' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Real-Time Macro Feed</span>
        </div>
        {loading && <Loader2 size={10} className='text-text-dim animate-spin' />}
      </div>
      <div className='flex-1 overflow-auto flex flex-col scrollbar'>
        {newsList.length === 0 && !loading && (
          <div className='text-center py-8 text-text-dim italic text-xs'>No macro feeds available.</div>
        )}
        {newsList.map((news, i) => (
          <div key={i} className='p-3 border-b border-border/50 hover:bg-hover transition-colors'>
            <div className='flex justify-between text-[10px] mb-1 font-mono'>
              <span className='text-accent-blue'>{news.time}</span>
              <span className={'uppercase font-bold ' + (news.impact === 'high' ? 'text-accent-red' : news.impact === 'medium' ? 'text-accent-amber' : 'text-accent-green')}>{news.impact} IMPACT</span>
            </div>
            <p className='text-xs text-white leading-relaxed'>{news.text}</p>
          </div>
        ))}
      </div>
    </div>
  );
}