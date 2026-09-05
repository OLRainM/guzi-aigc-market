type Props = {
  label?: string;
  fullscreen?: boolean;
};

export function LoadingSpinner({ label = '正在加载…', fullscreen = false }: Props) {
  return (
    <div className={fullscreen ? 'loading-fullscreen' : 'loading-inline'} role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}
