/** AgentKiln's decorative avatar, with idle glances and nearby mouse tracking. */
import { useEffect, useRef } from "react";

/** Render an unfocusable avatar that becomes still when reduced motion is enabled. */
export function AssistantAvatar({ className = "", trackPointer = true }) {
  const avatarRef = useRef(null);

  useEffect(() => {
    if (!trackPointer) return;
    const avatar = avatarRef.current;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;

    function reset() {
      cancelAnimationFrame(frame);
      frame = 0;
      delete avatar.dataset.following;
      avatar.style.removeProperty("--gaze-x");
      avatar.style.removeProperty("--gaze-y");
    }

    function follow(event) {
      if (event.pointerType !== "mouse" || reducedMotion.matches) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        frame = 0;
        const rect = avatar.getBoundingClientRect();
        const dx = event.clientX - rect.left - rect.width / 2;
        const dy = event.clientY - rect.top - rect.height / 2;
        const distance = Math.hypot(dx, dy);
        if (distance > 140 || rect.bottom < 0 || rect.top > window.innerHeight) {
          reset();
          return;
        }
        const scale = Math.min(distance / 48, 1) / Math.max(distance, 1);
        avatar.dataset.following = "true";
        avatar.style.setProperty("--gaze-x", `${dx * scale * 4}px`);
        avatar.style.setProperty("--gaze-y", `${dy * scale * 3}px`);
      });
    }

    function leave(event) {
      if (!event.relatedTarget) reset();
    }

    window.addEventListener("pointermove", follow, { passive: true });
    window.addEventListener("pointerout", leave);
    window.addEventListener("blur", reset);
    window.addEventListener("resize", reset);
    window.addEventListener("scroll", reset, true);
    document.addEventListener("visibilitychange", reset);
    reducedMotion.addEventListener("change", reset);
    return () => {
      reset();
      window.removeEventListener("pointermove", follow);
      window.removeEventListener("pointerout", leave);
      window.removeEventListener("blur", reset);
      window.removeEventListener("resize", reset);
      window.removeEventListener("scroll", reset, true);
      document.removeEventListener("visibilitychange", reset);
      reducedMotion.removeEventListener("change", reset);
    };
  }, [trackPointer]);

  return <span ref={avatarRef} className={`avatar avatar-assistant ${className}`.trim()} aria-hidden="true">
    <span className="assistant-gaze">
      <span className="assistant-eyes"><span /><span /></span>
    </span>
  </span>;
}
