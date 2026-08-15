import type { ReactNode, SVGProps } from 'react'

export type IconProps = { className?: string }

function Svg({ children, className = 'h-4 w-4', ...rest }: SVGProps<SVGSVGElement> & { children: ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.4}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...rest}
    >
      {children}
    </svg>
  )
}

export function FishIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M2 12c4-4.5 7-6.5 11-6.5 3 0 5.5 2 9 6.5-3.5 4.5-6 6.5-9 6.5-4 0-7-2-11-6.5z" />
      <circle cx="15.5" cy="12" r="1" fill="currentColor" stroke="none" />
      <path d="M6 12H3" />
    </Svg>
  )
}

export function DashboardIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="3" y="3" width="7.5" height="9" rx="1.5" />
      <rect x="13.5" y="3" width="7.5" height="5.5" rx="1.5" />
      <rect x="13.5" y="12" width="7.5" height="9" rx="1.5" />
      <rect x="3" y="15.5" width="7.5" height="5.5" rx="1.5" />
    </Svg>
  )
}

export function PlusCircleIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8v8M8 12h8" />
    </Svg>
  )
}

export function PlusIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M12 5v14M5 12h14" />
    </Svg>
  )
}

export function ServerIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="3" y="4" width="18" height="7" rx="2" />
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M7 7.5h.01M7 16.5h.01" />
    </Svg>
  )
}

export function GlobeIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3c2.5 2.6 2.5 15.4 0 18M12 3c-2.5 2.6-2.5 15.4 0 18" />
    </Svg>
  )
}

export function StoreIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M4 9l1-5h14l1 5" />
      <path d="M5 9a2.5 2.5 0 005 0 2.5 2.5 0 005 0 2.5 2.5 0 005 0M4 9v11h16V9" />
    </Svg>
  )
}

export function CreditCardIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="2.5" y="5" width="19" height="14" rx="2" />
      <path d="M2.5 10h19M6.5 15h4" />
    </Svg>
  )
}

export function HeadphonesIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M4 14v-2a8 8 0 0116 0v2" />
      <rect x="2.5" y="14" width="4.5" height="6" rx="1.5" />
      <rect x="17" y="14" width="4.5" height="6" rx="1.5" />
    </Svg>
  )
}

export function ShieldIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M12 3l7 3v5c0 4.5-3 8.5-7 10-4-1.5-7-5.5-7-10V6l7-3z" />
    </Svg>
  )
}

export function LogOutIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4M16 17l5-5-5-5M21 12H9" />
    </Svg>
  )
}

export function SunIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </Svg>
  )
}

export function MoonIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
    </Svg>
  )
}

export function CpuIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="6" y="6" width="12" height="12" rx="1.5" />
      <path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3" />
    </Svg>
  )
}

export function MemoryIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="4" y="6" width="16" height="12" rx="1.5" />
      <path d="M8 10v4M12 10v4M16 10v4M6 6V4M10 6V4M14 6V4M18 6V4M6 20v-2M10 20v-2M14 20v-2M18 20v-2" />
    </Svg>
  )
}

export function HardDriveIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M3 12h18M5 8h14l2 4v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4l2-4zM7 16h.01M11 16h.01" />
    </Svg>
  )
}

export function WalletIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M3 7a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2V7zm0 3h18M16 14h2" />
    </Svg>
  )
}

export function BellIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M18 8a6 6 0 00-12 0c0 7-3 8-3 8h18s-3-1-3-8M13.7 20a2 2 0 01-3.4 0" />
    </Svg>
  )
}

export function SearchIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4.3-4.3" />
    </Svg>
  )
}

export function CheckIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M20 6L9 17l-5-5" />
    </Svg>
  )
}

export function CopyIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
    </Svg>
  )
}

export function PasteIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M16 4h2a2 2 0 012 2v14a2 2 0 01-2 2H6a2 2 0 01-2-2V6a2 2 0 012-2h2" />
      <rect x="8" y="2" width="8" height="4" rx="1" />
    </Svg>
  )
}

export function XIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M18 6L6 18M6 6l12 12" />
    </Svg>
  )
}

export function ArrowRightIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M5 12h14M12 5l7 7-7 7" />
    </Svg>
  )
}

export function ArrowLeftIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M19 12H5M12 19l-7-7 7-7" />
    </Svg>
  )
}

export function RefreshIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M21 12a9 9 0 11-2.6-6.4M21 3v6h-6" />
    </Svg>
  )
}

export function TerminalIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="2.5" y="4" width="19" height="16" rx="2" />
      <path d="M6 9l3 3-3 3M11.5 15H17" />
    </Svg>
  )
}

export function SendIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z" />
    </Svg>
  )
}

export function MailIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="2.5" y="5" width="19" height="14" rx="2" />
      <path d="M3 7l9 6 9-6" />
    </Svg>
  )
}

export function EyeIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" />
      <circle cx="12" cy="12" r="3" />
    </Svg>
  )
}

export function EyeOffIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M17.9 17.9A10.7 10.7 0 0112 19c-6.5 0-10-7-10-7a20 20 0 015-5.5M9.9 4.2A9.7 9.7 0 0112 4c6.5 0 10 7 10 7a19.6 19.6 0 01-3.5 4.5M1 1l22 22M9.9 9.9a3 3 0 004.2 4.2" />
    </Svg>
  )
}

export function CoinsIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="8" cy="8" r="6" />
      <path d="M18 9a6 6 0 01-9 5.2M21.5 14.5A6 6 0 0114 21" />
    </Svg>
  )
}

export function TagIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M20.6 13.4L11 3.8A2 2 0 009.6 3.2H4a1 1 0 00-1 1v5.6c0 .5.2 1 .6 1.4l9.6 9.6a2 2 0 002.8 0l4.6-4.6a2 2 0 000-2.8z" />
      <circle cx="7.5" cy="7.5" r="1.2" fill="currentColor" stroke="none" />
    </Svg>
  )
}

export function PowerIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M12 2v9M5 6a9 9 0 1014 0" />
    </Svg>
  )
}

export function RotateCwIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M21 12a9 9 0 11-3-6.7M21 3v6h-6" />
    </Svg>
  )
}

export function TrashIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M3 6h18M8 6V4a1 1 0 011-1h6a1 1 0 011 1v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6M10 11v6M14 11v6" />
    </Svg>
  )
}

export function KeyIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="7.5" cy="15.5" r="4.5" />
      <path d="M10.7 12.3L21 2M15 8l3 3M18 5l2 2" />
    </Svg>
  )
}

export function ChevronDownIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M6 9l6 6 6-6" />
    </Svg>
  )
}

export function InfoIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8h.01M11 12h1v5h1" />
    </Svg>
  )
}

export function ShieldCheckIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <path d="M12 3l7 3v5c0 4.5-3 8.5-7 10-4-1.5-7-5.5-7-10V6l7-3z" />
      <path d="M9 12l2 2 4-4" />
    </Svg>
  )
}

export function AlertIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v6M12 17h.01" />
    </Svg>
  )
}

export function CalendarIcon({ className }: IconProps) {
  return (
    <Svg className={className}>
      <rect x="3" y="4.5" width="18" height="17" rx="2" />
      <path d="M3 9h18M8 2.5v4M16 2.5v4M7.5 14h.01M12 14h.01M16.5 14h.01M7.5 18h.01M12 18h.01" />
    </Svg>
  )
}
