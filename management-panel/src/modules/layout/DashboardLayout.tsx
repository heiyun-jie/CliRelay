import { Outlet, useLocation } from "react-router-dom";
import { AppShell } from "@/modules/ui/AppShell";
import { Reveal } from "@/modules/ui/Reveal";

export function DashboardLayout() {
  const location = useLocation();
  return (
    <AppShell>
      <Reveal key={location.pathname}>
        <Outlet />
      </Reveal>
    </AppShell>
  );
}
