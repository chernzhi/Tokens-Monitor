import React, { useRef, useEffect, useState } from "react";

interface Props {
  children: React.ReactNode;
  speed?: number; // px per second
  pauseOnHover?: boolean;
}

/**
 * Vertical auto-scroll container for dashboard lists.
 * If content fits, no scroll. If overflows, smoothly loops.
 * Wheel input pauses auto-scroll for ~4s and lets the user scroll freely.
 */
export default function AutoScroll({ children, speed = 20, pauseOnHover = true }: Props) {
  const outerRef = useRef<HTMLDivElement>(null);
  const innerRef = useRef<HTMLDivElement>(null);
  const [needsScroll, setNeedsScroll] = useState(false);
  const [paused, setPaused] = useState(false);
  const [compactMode, setCompactMode] = useState(false);
  const [manualScroll, setManualScroll] = useState(false);
  const offset = useRef(0);
  const raf = useRef(0);
  const lastTime = useRef(0);
  const manualTimer = useRef<number | null>(null);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const media = window.matchMedia("(max-width: 768px)");
    const updateCompactMode = () => setCompactMode(media.matches);
    updateCompactMode();
    if (typeof media.addEventListener === "function") {
      media.addEventListener("change", updateCompactMode);
      return () => media.removeEventListener("change", updateCompactMode);
    }
    media.addListener(updateCompactMode);
    return () => media.removeListener(updateCompactMode);
  }, []);

  useEffect(() => {
    const outer = outerRef.current;
    const inner = innerRef.current;
    if (!outer || !inner) return;
    const check = () => setNeedsScroll(inner.scrollHeight > outer.clientHeight + 2);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(outer);
    ro.observe(inner);
    return () => ro.disconnect();
  }, [children]);

  const shouldAnimate = needsScroll && !compactMode && !manualScroll;

  useEffect(() => {
    const inner = innerRef.current;
    if (!shouldAnimate || paused) {
      cancelAnimationFrame(raf.current);
      lastTime.current = 0;
      if (!needsScroll && inner) {
        offset.current = 0;
        inner.style.transform = "translateY(0)";
      }
      return;
    }
    if (!inner) return;

    const step = (now: number) => {
      const halfH = inner.scrollHeight / 2;
      if (halfH <= 0) {
        lastTime.current = now;
        raf.current = requestAnimationFrame(step);
        return;
      }
      if (lastTime.current) {
        const dt = (now - lastTime.current) / 1000;
        offset.current += speed * dt;
        if (offset.current >= halfH) offset.current -= halfH;
        inner.style.transform = `translateY(-${offset.current}px)`;
      }
      lastTime.current = now;
      raf.current = requestAnimationFrame(step);
    };
    raf.current = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf.current);
  }, [paused, shouldAnimate, speed, needsScroll]);

  // Wheel: pause auto-scroll, let user drive offset directly. Resume after idle.
  useEffect(() => {
    const outer = outerRef.current;
    if (!outer || !needsScroll || compactMode) return;
    const onWheel = (e: WheelEvent) => {
      const inner = innerRef.current;
      if (!inner) return;
      e.preventDefault();
      const fullH = manualScroll ? inner.scrollHeight : inner.scrollHeight / 2;
      const max = Math.max(fullH - outer.clientHeight, 0);
      let next = offset.current + e.deltaY;
      next = Math.max(0, Math.min(next, max));
      offset.current = next;
      inner.style.transform = `translateY(-${next}px)`;
      if (!manualScroll) setManualScroll(true);
      if (manualTimer.current) window.clearTimeout(manualTimer.current);
      manualTimer.current = window.setTimeout(() => {
        setManualScroll(false);
        lastTime.current = 0;
      }, 4000);
    };
    outer.addEventListener("wheel", onWheel, { passive: false });
    return () => outer.removeEventListener("wheel", onWheel);
  }, [needsScroll, compactMode, manualScroll]);

  return (
    <div
      ref={outerRef}
      className={`auto-scroll-outer${compactMode ? " is-compact" : ""}`}
      onMouseEnter={() => !compactMode && pauseOnHover && setPaused(true)}
      onMouseLeave={() => {
        setPaused(false);
        lastTime.current = 0;
      }}
    >
      <div ref={innerRef} className="auto-scroll-inner">
        {children}
        {/* 内容超出可视区域时复制一份做无缝循环；手动滚动或不超出时不复制 */}
        {shouldAnimate ? children : null}
      </div>
    </div>
  );
}
