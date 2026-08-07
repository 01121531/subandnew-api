/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from "@tanstack/react-router";
import { ArrowUp } from "lucide-react";
import { Fragment, useMemo } from "react";
import { useTranslation } from "react-i18next";

import { BrandMark, BrandWordmark } from "@/components/brand-wordmark";
import { useStatus } from "@/hooks/use-status";
import { useSystemConfig } from "@/hooks/use-system-config";
import { isHuichuanBrand } from "@/lib/brand";
import { cn } from "@/lib/utils";

interface FooterLink {
  text: string;
  href: string;
}

interface FooterColumnProps {
  title: string;
  links: FooterLink[];
}

interface FooterProps {
  logo?: string;
  name?: string;
  columns?: FooterColumnProps[];
  copyright?: string;
  className?: string;
}

function FooterLinkItem(props: { link: FooterLink }) {
  const { t } = useTranslation();
  const isExternal = props.link.href.startsWith("http");
  const label = t(props.link.text);

  if (isExternal) {
    return (
      <a
        href={props.link.href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-muted-foreground hover:text-foreground text-sm transition-colors duration-200"
      >
        {label}
      </a>
    );
  }

  return (
    <Link
      to={props.link.href}
      className="text-muted-foreground hover:text-foreground text-sm transition-colors duration-200"
    >
      {label}
    </Link>
  );
}

// Renders User Agreement / Privacy Policy links when either is configured in
// System Settings. By default, emits inline siblings for the copyright row.
function LegalLinks(props: {
  leadingSeparator?: boolean;
  className?: string;
  inverse?: boolean;
}) {
  const { t } = useTranslation();
  const { status } = useStatus();
  const items: { key: string; label: string; href: string }[] = [];
  if (status?.user_agreement_enabled) {
    items.push({
      key: "user-agreement",
      label: t("User Agreement"),
      href: "/user-agreement",
    });
  }
  if (status?.privacy_policy_enabled) {
    items.push({
      key: "privacy-policy",
      label: t("Privacy Policy"),
      href: "/privacy-policy",
    });
  }
  if (items.length === 0) {
    return null;
  }
  const content = (
    <>
      {items.map((item, index) => (
        <Fragment key={item.key}>
          {(props.leadingSeparator || index > 0) && (
            <span
              aria-hidden="true"
              className={
                props.inverse
                  ? "text-muted-foreground/25 dark:text-white/20"
                  : "text-muted-foreground/30"
              }
            >
              ·
            </span>
          )}
          <Link
            to={item.href}
            className={cn(
              "transition-colors duration-200",
              props.inverse
                ? "text-muted-foreground hover:text-foreground dark:text-white/45 dark:hover:text-white"
                : "hover:text-foreground",
            )}
          >
            {item.label}
          </Link>
        </Fragment>
      ))}
    </>
  );

  return props.className ? (
    <div className={props.className}>{content}</div>
  ) : (
    content
  );
}

export function Footer(props: FooterProps) {
  const { t } = useTranslation();
  const {
    systemName,
    logo: systemLogo,
    footerHtml,
    demoSiteEnabled,
  } = useSystemConfig();

  const displayLogo = systemLogo || props.logo || "/logo.png";
  const displayName = systemName || props.name || "HUICHUAN";
  const isDemoSiteMode = Boolean(demoSiteEnabled);
  const currentYear = new Date().getFullYear();

  const fallbackColumns = useMemo<FooterColumnProps[]>(
    () => [
      {
        title: t("footer.columns.about.title"),
        links: [
          {
            text: t("footer.columns.about.links.aboutProject"),
            href: "https://github.com/01121531/subandnew-api",
          },
          {
            text: t("footer.columns.about.links.contact"),
            href: "https://github.com/01121531/subandnew-api",
          },
          {
            text: t("footer.columns.about.links.features"),
            href: "https://github.com/01121531/subandnew-api",
          },
        ],
      },
      {
        title: t("footer.columns.docs.title"),
        links: [
          {
            text: t("footer.columns.docs.links.quickStart"),
            href: "https://github.com/01121531/subandnew-api",
          },
          {
            text: t("footer.columns.docs.links.installation"),
            href: "https://github.com/01121531/subandnew-api",
          },
          {
            text: t("footer.columns.docs.links.apiDocs"),
            href: "https://github.com/01121531/subandnew-api",
          },
        ],
      },
      {
        title: t("footer.columns.related.title"),
        links: [
          {
            text: t("footer.columns.related.links.oneApi"),
            href: "https://github.com/songquanpeng/one-api",
          },
          {
            text: t("footer.columns.related.links.midjourney"),
            href: "https://github.com/novicezk/midjourney-proxy",
          },
          {
            text: t("footer.columns.related.links.huichuanKeyTool"),
            href: "https://github.com/01121531/subandnew-api",
          },
        ],
      },
    ],
    [t],
  );

  const displayColumns = props.columns ?? fallbackColumns;

  if (footerHtml) {
    return (
      <footer
        className={cn(
          "border-border/40 relative z-10 border-t",
          props.className,
        )}
      >
        <div className="mx-auto w-full max-w-6xl px-6 py-5">
          <div className="bg-muted/20 border-border/50 flex flex-col items-center justify-between gap-4 rounded-2xl border px-4 py-4 backdrop-blur-sm sm:flex-row sm:px-5">
            <div
              className="custom-footer text-muted-foreground min-w-0 text-center text-sm sm:text-left"
              dangerouslySetInnerHTML={{ __html: footerHtml }}
            />
            <LegalLinks className="border-border/60 text-muted-foreground flex w-full flex-wrap items-center justify-center gap-x-3 gap-y-1 border-t pt-4 text-xs sm:w-auto sm:justify-end sm:border-t-0 sm:border-l sm:pt-0 sm:pl-5" />
          </div>
        </div>
      </footer>
    );
  }

  if (displayColumns.length === 0) {
    return (
      <footer
        className={cn(
          "border-border/60 bg-background text-foreground relative z-10 overflow-hidden border-t dark:border-white/8 dark:bg-zinc-950 dark:text-white",
          props.className,
        )}
      >
        <div
          aria-hidden="true"
          className="absolute inset-0 bg-[linear-gradient(to_right,rgba(15,23,42,.035)_1px,transparent_1px),linear-gradient(to_bottom,rgba(15,23,42,.035)_1px,transparent_1px)] [mask-image:linear-gradient(to_bottom,black,transparent_85%)] bg-[size:52px_52px] dark:bg-[linear-gradient(to_right,rgba(255,255,255,.035)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,.035)_1px,transparent_1px)]"
        />
        <div
          aria-hidden="true"
          className="absolute -top-40 left-1/2 h-80 w-[52rem] -translate-x-1/2 rounded-full bg-violet-500/8 blur-[110px] dark:bg-violet-600/18"
        />
        <div
          aria-hidden="true"
          className="absolute -right-24 -bottom-40 size-80 rounded-full bg-fuchsia-500/6 blur-[100px] dark:bg-fuchsia-500/10"
        />

        <div className="relative mx-auto max-w-6xl px-6 py-9 sm:py-10">
          <div className="flex flex-col gap-8 md:flex-row md:items-center md:justify-between">
            <a href="#home" className="group flex items-center gap-3.5">
              <span className="border-border/70 bg-card/80 flex size-11 items-center justify-center rounded-2xl border p-1.5 shadow-[0_12px_36px_-16px_rgba(139,92,246,.35)] transition-transform duration-300 group-hover:-rotate-6 group-hover:scale-105 dark:border-white/10 dark:bg-white/[0.055] dark:shadow-[0_12px_36px_-16px_rgba(139,92,246,.7)]">
                {isHuichuanBrand(displayName) ? (
                  <BrandMark className="size-full" />
                ) : (
                  <img
                    src={displayLogo}
                    alt=""
                    className="size-full rounded-xl object-contain"
                  />
                )}
              </span>
              <span>
                {isHuichuanBrand(displayName) ? (
                  <BrandWordmark
                    name={displayName}
                    className="text-foreground text-base dark:text-white"
                  />
                ) : (
                  <span className="text-base font-semibold tracking-tight">
                    {displayName}
                  </span>
                )}
                <span className="text-muted-foreground mt-1 block text-xs dark:text-white/40">
                  {t("Powerful API Management Platform")}
                </span>
              </span>
            </a>

            <nav
              aria-label={t("Primary navigation")}
              className="flex flex-wrap items-center gap-x-1 gap-y-2"
            >
              {[
                ["Home", "#home"],
                ["Core capabilities", "#capabilities"],
                ["Use cases", "#scenarios"],
                ["Frequently asked questions", "#faq"],
              ].map(([label, href]) => (
                <a
                  key={href}
                  href={href}
                  className="text-muted-foreground hover:bg-muted hover:text-foreground rounded-lg px-3 py-2 text-xs font-medium transition-colors duration-200 dark:text-white/45 dark:hover:bg-white/[0.055] dark:hover:text-white"
                >
                  {t(label)}
                </a>
              ))}
            </nav>

            <a
              href="#home"
              aria-label={t("Home")}
              className="border-border/70 bg-card/80 text-muted-foreground hover:text-foreground group flex size-11 shrink-0 items-center justify-center rounded-full border transition-all duration-300 hover:-translate-y-1 hover:border-violet-400/40 hover:bg-violet-500/10 dark:border-white/12 dark:bg-white/[0.055] dark:text-white/65 dark:hover:bg-violet-500/15 dark:hover:text-white"
            >
              <ArrowUp className="size-4 transition-transform duration-300 group-hover:-translate-y-0.5" />
            </a>
          </div>

          <div className="border-border/60 text-muted-foreground mt-8 flex flex-col gap-3 border-t pt-5 text-xs sm:flex-row sm:items-center sm:justify-between dark:border-white/8 dark:text-white/35">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span>
                &copy; {currentYear} {displayName}.{" "}
                {props.copyright ?? t("footer.defaultCopyright")}
              </span>
              <LegalLinks leadingSeparator inverse />
            </div>
            <span className="text-muted-foreground/60 font-mono tracking-[0.16em] uppercase dark:text-white/25">
              API · ROUTING · OBSERVABILITY
            </span>
          </div>
        </div>
      </footer>
    );
  }

  return (
    <footer
      className={cn("border-border/40 relative z-10 border-t", props.className)}
    >
      <div className="mx-auto max-w-6xl px-6 py-12 md:py-16">
        <div className="flex flex-col justify-between gap-10 md:flex-row md:gap-16">
          {/* Brand column */}
          <div className="shrink-0">
            <Link to="/" className="group flex items-center gap-2.5">
              {isHuichuanBrand(displayName) ? (
                <>
                  <BrandMark className="size-7" />
                  <BrandWordmark name={displayName} className="text-sm" />
                </>
              ) : (
                <>
                  <img
                    src={displayLogo}
                    alt={displayName}
                    className="size-7 rounded-lg object-contain"
                  />
                  <span className="text-sm font-semibold tracking-tight">
                    {displayName}
                  </span>
                </>
              )}
            </Link>
            <p className="text-muted-foreground mt-3 max-w-[200px] text-xs leading-relaxed">
              {t("Powerful API Management Platform")}
            </p>
          </div>

          {/* Links columns */}
          {isDemoSiteMode && (
            <div className="grid grid-cols-3 gap-8 md:gap-16">
              {displayColumns.map((column) => (
                <div key={column.title}>
                  <p className="text-muted-foreground/50 mb-3 text-xs font-medium tracking-wider uppercase">
                    {t(column.title)}
                  </p>
                  <ul className="space-y-2.5">
                    {column.links.map((link) => (
                      <li key={link.href}>
                        <FooterLinkItem link={link} />
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Copyright + optional legal links. */}
        <div className="border-border/30 mt-12 flex flex-col items-center justify-between gap-x-3 gap-y-2 border-t pt-6 sm:flex-row">
          <div className="text-muted-foreground flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-xs sm:justify-start">
            <span>
              &copy; {currentYear} {displayName}.{" "}
              {props.copyright ?? t("footer.defaultCopyright")}
            </span>
            <LegalLinks leadingSeparator />
          </div>
        </div>
      </div>
    </footer>
  );
}
