import React, { Component, ErrorInfo, ReactNode } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';

interface Props {
  children: ReactNode;
  title?: string;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export default class ErrorBoundary extends Component<Props, State> {
  public state: State = {
    hasError: false,
    error: null,
  };

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('Uncaught error in component:', error, errorInfo);
  }

  private handleReset = () => {
    this.setState({ hasError: false, error: null });
  };

  public render() {
    if (this.state.hasError) {
      return (
        <div className="bg-panel border border-accent-red/30 rounded-lg p-4 h-full flex flex-col justify-center items-center text-center shadow-lg">
          <AlertTriangle size={28} className="text-accent-red mb-2 animate-pulse" />
          <h3 className="text-sm font-bold text-white mb-1">
            {this.props.title ? `${this.props.title} Error` : 'Panel Error'}
          </h3>
          <p className="text-[11px] text-text-dim max-w-xs mb-3 font-mono leading-relaxed truncate w-full">
            {this.state.error?.message || 'An unexpected rendering error occurred.'}
          </p>
          <button
            onClick={this.handleReset}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent-blue/20 hover:bg-accent-blue/30 text-accent-blue text-xs rounded font-semibold transition-colors"
          >
            <RefreshCw size={12} />
            <span>Reload Panel</span>
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}
