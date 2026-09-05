import { Component, type ErrorInfo, type ReactNode } from 'react';

type Props = { children: ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('应用渲染失败', error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <main className="error-page" role="alert">
          <p className="eyebrow">UNEXPECTED ERROR</p>
          <h1>页面暂时无法显示</h1>
          <p className="lead">应用遇到意外错误，请刷新页面后重试。</p>
          <button type="button" onClick={() => window.location.reload()}>刷新页面</button>
        </main>
      );
    }
    return this.props.children;
  }
}
