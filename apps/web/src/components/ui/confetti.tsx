import { useEffect, useState } from "react";
import ReactConfetti from "react-confetti";

/** forma2's Confetti: one burst over the whole window, which does not take pointer events. */
export const Confetti = ({ colors = ["#D3264A", "#eee"] }: { colors?: string[] }) => {
  const [windowSize, setWindowSize] = useState({ height: 0, width: 0 });

  useEffect(() => {
    const updateWindowSize = () => setWindowSize({ height: window.innerHeight, width: window.innerWidth });
    updateWindowSize();
    window.addEventListener("resize", updateWindowSize);
    return () => window.removeEventListener("resize", updateWindowSize);
  }, []);

  return (
    <ReactConfetti
      width={windowSize.width}
      height={windowSize.height}
      colors={colors}
      numberOfPieces={400}
      recycle={false}
      style={{ position: "fixed", top: 0, left: 0, zIndex: 9999, pointerEvents: "none" }}
    />
  );
};
