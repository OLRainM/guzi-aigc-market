import { Component, Suspense, useEffect, useMemo, useRef, useState, type MutableRefObject, type ReactNode } from 'react';
import { Canvas, useThree } from '@react-three/fiber';
import { Html, OrbitControls, useGLTF, useProgress } from '@react-three/drei';
import { Box3, Vector3, type Object3D } from 'three';
import type { OrbitControls as OrbitControlsImpl } from 'three-stdlib';
import { Maximize2, Minimize2, RotateCcw } from 'lucide-react';
import { MAX_MODEL_BYTES, assetSrc, formatMegabytes, requestBlob, type AssetFile } from './api';

type Props = {
  model: AssetFile;
  fallbackImages?: AssetFile[];
  compact?: boolean;
};

type ViewerErrorBoundaryProps = { resetKey: string; onError: (message: string) => void; children: ReactNode };

class ViewerErrorBoundary extends Component<ViewerErrorBoundaryProps, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch() {
    this.props.onError('模型无法解析，已切换为图片预览');
  }

  componentDidUpdate(prevProps: ViewerErrorBoundaryProps) {
    if (prevProps.resetKey !== this.props.resetKey && this.state.failed) {
      this.setState({ failed: false });
    }
  }

  render() {
    return this.state.failed ? null : this.props.children;
  }
}

function disposeObject(root: Object3D) {
  root.traverse(object => {
    const mesh = object as Object3D & { geometry?: { dispose: () => void }; material?: { dispose: () => void } | Array<{ dispose: () => void }> };
    mesh.geometry?.dispose();
    if (Array.isArray(mesh.material)) {
      mesh.material.forEach(item => item.dispose());
    } else {
      mesh.material?.dispose();
    }
  });
}

function FittedModel({ url, onReady }: { url: string; onReady: () => void }) {
  const { scene } = useGLTF(url);
  const ready = useRef(onReady);
  ready.current = onReady;
  const model = useMemo(() => {
    const cloned = scene.clone(true);
    let meshCount = 0;
    cloned.traverse(object => {
      if ((object as { isMesh?: boolean }).isMesh) meshCount += 1;
    });
    if (meshCount === 0) {
      throw new Error('empty model');
    }
    const box = new Box3().setFromObject(cloned);
    const size = box.getSize(new Vector3());
    const maxDim = Math.max(size.x, size.y, size.z, 0.0001);
    cloned.scale.setScalar(1.7 / maxDim);
    const fitted = new Box3().setFromObject(cloned);
    cloned.position.sub(fitted.getCenter(new Vector3()));
    return cloned;
  }, [scene]);

  useEffect(() => {
    ready.current();
    return () => {
      disposeObject(model);
      useGLTF.clear(url);
    };
  }, [model, url]);

  return <primitive object={model} />;
}

function LoadProgress({ visible }: { visible: boolean }) {
  const { progress, active } = useProgress();
  if (!visible) return null;
  const percent = active ? Math.round(progress) : 8;
  return (
    <Html fullscreen zIndexRange={[10, 0]}>
      <div className="viewer-progress" role="status">
        <span>正在加载 3D 模型</span>
        <strong>{percent}%</strong>
        <i style={{ width: `${Math.max(percent, 6)}%` }} />
      </div>
    </Html>
  );
}

function CameraRig({ controlsRef }: { controlsRef: MutableRefObject<OrbitControlsImpl | null> }) {
  const { camera } = useThree();
  useEffect(() => {
    camera.position.set(0, 0.45, 2.5);
    camera.lookAt(0, 0, 0);
  }, [camera]);
  return (
    <OrbitControls
      ref={controlsRef}
      makeDefault
      enableDamping
      dampingFactor={0.08}
      minDistance={0.7}
      maxDistance={8}
      maxPolarAngle={Math.PI * 0.92}
    />
  );
}

export function ModelViewer({ model, fallbackImages = [], compact = false }: Props) {
  const shellRef = useRef<HTMLDivElement>(null);
  const controlsRef = useRef<OrbitControlsImpl | null>(null);
  const [blobUrl, setBlobUrl] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const node = shellRef.current;
    if (!node) return;
    const sync = () => setFullscreen(document.fullscreenElement === node);
    document.addEventListener('fullscreenchange', sync);
    return () => document.removeEventListener('fullscreenchange', sync);
  }, []);

  useEffect(() => {
    let revoked = '';
    let cancelled = false;
    setError('');
    setBlobUrl('');
    setLoading(true);
    if (model.size_bytes > MAX_MODEL_BYTES) {
      setError(`模型 ${formatMegabytes(model.size_bytes)}，超过 20 MB 预览上限`);
      setLoading(false);
      return;
    }
    requestBlob(model.content_url)
      .then(blob => {
        if (cancelled) return;
        if (blob.size > MAX_MODEL_BYTES) {
          setError('模型超过 20 MB，无法在网页中预览');
          setLoading(false);
          return;
        }
        revoked = URL.createObjectURL(blob);
        setBlobUrl(revoked);
      })
      .catch(() => {
        if (!cancelled) {
          setError('模型下载失败，已切换为图片预览');
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
      if (revoked) URL.revokeObjectURL(revoked);
    };
  }, [model.id, model.content_url, model.size_bytes]);

  const resetView = () => {
    const controls = controlsRef.current;
    if (!controls) return;
    controls.object.position.set(0, 0.45, 2.5);
    controls.target.set(0, 0, 0);
    controls.update();
  };

  const toggleFullscreen = async () => {
    const node = shellRef.current;
    if (!node) return;
    try {
      if (document.fullscreenElement === node) {
        await document.exitFullscreen();
      } else {
        await node.requestFullscreen();
      }
    } catch {
      setError(current => current || '当前浏览器不支持全屏预览');
    }
  };

  const fallback = fallbackImages[0];
  const showFallback = Boolean(error);

  return (
    <section ref={shellRef} className={`model-viewer${compact ? ' compact' : ''}${fullscreen ? ' is-fullscreen' : ''}`} aria-label="3D 模型预览">
      <div className="viewer-stage">
        {blobUrl && !showFallback && (
          <ViewerErrorBoundary resetKey={blobUrl} onError={message => { setError(message); setLoading(false); }}>
            <Canvas camera={{ position: [0, 0.45, 2.5], fov: 42 }} dpr={[1, 2]} gl={{ antialias: true }}>
              <color attach="background" args={['#16111f']} />
              <ambientLight intensity={0.85} />
              <directionalLight position={[4, 6, 3]} intensity={1.15} />
              <directionalLight position={[-4, 1.5, -2]} intensity={0.35} />
              <Suspense fallback={null}>
                <FittedModel url={blobUrl} onReady={() => setLoading(false)} />
              </Suspense>
              <CameraRig controlsRef={controlsRef} />
              <LoadProgress visible={loading} />
            </Canvas>
          </ViewerErrorBoundary>
        )}
        {showFallback && (
          fallback
            ? <img className="viewer-fallback" src={assetSrc(fallback)} alt="模型预览降级图片" />
            : <div className="placeholder-cover viewer-fallback">暂无可以降级展示的图片</div>
        )}
        {!showFallback && loading && !blobUrl && (
          <div className="viewer-progress" role="status">
            <span>正在准备 3D 预览</span>
            <strong>…</strong>
          </div>
        )}
      </div>
      <div className="viewer-toolbar">
        <p className="muted">{model.original_name} · {formatMegabytes(model.size_bytes)} · 拖拽旋转，滚轮缩放</p>
        <div className="viewer-actions">
          <button type="button" className="ghost" onClick={resetView} disabled={showFallback} aria-label="重置视角">
            <RotateCcw size={16} /> 重置视角
          </button>
          <button type="button" className="ghost" onClick={() => void toggleFullscreen()} aria-label={fullscreen ? '退出全屏' : '全屏预览'}>
            {fullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />} {fullscreen ? '退出全屏' : '全屏'}
          </button>
        </div>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
    </section>
  );
}
