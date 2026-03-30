import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "@/modules/auth/AuthProvider";
import { PageBackground } from "@/modules/ui/PageBackground";

export function ProtectedRoute() {
  const location = useLocation();
  const {
    state: { isAuthenticated, isRestoring },
  } = useAuth();

  if (isRestoring) {
    return (
      <PageBackground variant="app">
        <div className="flex min-h-screen items-center justify-center">
          <div className="rounded-2xl border border-slate-200 bg-white/90 px-6 py-4 text-sm text-slate-700 shadow-sm backdrop-blur dark:border-neutral-800 dark:bg-neutral-950/70 dark:text-white/75">
            正在恢复会话...
          </div>
        </div>
      </PageBackground>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }

  return <Outlet />;
}
