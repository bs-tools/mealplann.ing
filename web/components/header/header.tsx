import { LogoMark, Wordmark } from "@components/header/logo";
import React from "react";
import { Link } from "react-router";

/**
 * Header is the brand above the pages seen signed out, like signing in and
 * the privacy policy. Signed in, the main pages are in the tab bar or the
 * side rail instead, so there's no top bar.
 */
const Header: React.FC = () => (
  <nav className="relative bg-ink font-ui print:hidden">
    <div className="mx-auto flex h-16 max-w-5xl items-center px-4">
      <Link to="/" className="flex shrink-0 items-center gap-2.5">
        <LogoMark />
        <Wordmark className="text-[15px] text-neutral-50 sm:text-lg" />
      </Link>
    </div>
  </nav>
);

export default Header;
