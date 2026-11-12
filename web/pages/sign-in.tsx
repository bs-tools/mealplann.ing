import { LogoMark, Wordmark } from "@components/header/logo";
import SignInForm from "@components/header/sign-in-form";
import React from "react";
import { Link } from "react-router";

/**
 * SignInPage is shown instead of the pages that need a user: the app's mark
 * and tagline on one side, and the passkey sign in on the other.
 */
const SignInPage: React.FC = () => (
  <div className="flex min-h-full flex-col bg-stone-50 md:flex-row font-ui text-ink">
    <section className="relative flex min-h-80 flex-col justify-between gap-8 overflow-hidden bg-ink px-6 py-8 text-neutral-50 sm:px-12 sm:py-10 md:flex-1">
      <div className="flex items-center gap-3">
        <LogoMark className="size-9" />
        <Wordmark className="text-[22px]" />
      </div>
      <div className="relative flex max-w-md flex-col gap-3 pb-20">
        <h1 className="text-3xl leading-tight font-bold sm:text-4xl">
          Eat well, together.
        </h1>
        <p className="text-base leading-relaxed text-neutral-300">
          Vegan meal planning for the whole household: recipes, weekly plans,
          and one shop a week.
        </p>
      </div>
      <svg
        aria-hidden="true"
        viewBox="0 0 600 120"
        preserveAspectRatio="none"
        className="absolute inset-x-0 bottom-0 h-[120px] w-full"
      >
        <path
          d="M0 96 C 80 70, 160 112, 260 84 S 460 64, 600 92"
          fill="none"
          className="stroke-leaf"
          strokeWidth="2"
          strokeOpacity="0.55"
          strokeLinecap="round"
        />
        <path
          d="M0 110 C 100 96, 180 116, 300 100 S 480 92, 600 106"
          fill="none"
          stroke="#404040"
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
      </svg>
    </section>
    <section className="flex flex-col items-center justify-center gap-6 px-6 py-10 md:flex-1">
      <SignInForm />
      <Link to="/privacy" className="text-sm text-stone-500 hover:underline">
        Privacy policy
      </Link>
    </section>
  </div>
);

export default SignInPage;
