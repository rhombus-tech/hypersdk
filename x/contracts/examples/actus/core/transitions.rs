use wasmlanche::borsh::{BorshDeserialize, BorshSerialize};

//use crate::token::Units;

use super::types::*;
use super::Error;
use super::Result;
use crate::anx_utils::process_initial_exchange;
use crate::anx_utils::process_interest_capitalization;
use crate::anx_utils::process_interest_payment;
use crate::anx_utils::process_maturity;
use crate::anx_utils::process_principal_redemption;
use crate::anx_utils::process_rate_reset;
use crate::anx_utils::process_scaling;

use crate::math;

pub struct TransitionEngine;
pub type Units = u64;
impl TransitionEngine {
    /// Main entry point for processing an ACTUS event
    pub fn process_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        // Validate event timing
        if timestamp < state.status_date {
            return Err(Error::TransitionError(
                "Event timestamp before status date".into(),
            ));
        }

        // Update accrued interest if time has passed
        if timestamp > state.status_date {
            update_accrued_interest(state, terms, timestamp)?;
        }

        // Dispatch based on contract type (PAM, LAM, NAM, ANN, etc.)
        let result = match terms.contract_type {
            ContractType::PAM => Self::process_pam_event(event, timestamp, state, terms),
            ContractType::LAM => Self::process_lam_event(event, timestamp, state, terms),
            ContractType::NAM => Self::process_nam_event(event, timestamp, state, terms),
            ContractType::ANN => Self::process_ann_event(event, timestamp, state, terms),
            ContractType::NAX => Self::process_nax_event(event, timestamp, state, terms),
            ContractType::ANX => Self::process_anx_event(event, timestamp, state, terms),
            ContractType::CLM => Self::process_clm_event(event, timestamp, state, terms),
            ContractType::LAX => Self::process_lax_event(event, timestamp, state, terms),
            ContractType::PBN => Self::process_pbn_event(event, timestamp, state, terms),
            // If you want to handle e.g. SWPPV, STK, CLM, etc., add them here:
            // ContractType::STK => Self::process_stk_event(event, timestamp, state, terms),
            // ...
            _ => Err(Error::TransitionError("Unsupported contract type".into())),
        }?;

        // Update status date
        state.status_date = timestamp;

        Ok(result)
    }

    // =======================
    //        PAM Logic
    // =======================
    fn process_pam_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            // Set up initial principal and interest
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Payment
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let payment = state.accrued_interest;
                    state.accrued_interest = 0;
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            }

            // Principal Redemption at Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let principal = state.notional_principal;
                        let interest = state.accrued_interest;
                        let total_payment = principal + interest;

                        // Zero out principal/interest
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Regular Monitoring/Accrual
            EventType::AD => {
                update_accrued_interest(state, terms, timestamp)?;
                Ok(None)
            }

            EventType::RR => {
                // If your contract logic supports `next_reset_rate`
                //if let Some(rate) = terms.next_reset_rate {
                //   state.nominal_interest_rate = rate;
                //}
                Ok(None)
            }

            // Fee Payment
            EventType::FP => {
                // If your contract has a fee_accrued or fee fields, pay them here
                // e.g., let paid_fee = state.fee_accrued; state.fee_accrued = 0; Ok(Some(paid_fee))
                Ok(None)
            }

            // Principal Prepayment
            EventType::PP => {
                if let Some(pp_effect) = terms.prepayment_effect {
                    match pp_effect {
                        PrepaymentEffect::PPEF_N => Ok(None), // No prepayment
                        PrepaymentEffect::PPEF_A => {
                            // Reduce next principal redemption, if any
                            if state.next_principal_redemption_payment > 0 {
                                let amount = state.next_principal_redemption_payment;
                                state.next_principal_redemption_payment = 0;
                                Ok(Some(amount))
                            } else {
                                Ok(None)
                            }
                        }
                        PrepaymentEffect::PPEF_M => {
                            // Wipe out entire principal
                            let amount = state.notional_principal;
                            state.notional_principal = 0;
                            Ok(Some(amount))
                        }
                    }
                } else {
                    Ok(None)
                }
            }

            // Additional event examples:
            // EventType::PD => { /* Principal Drawing logic, if any */ Ok(None) },
            // EventType::PY => { /* Penalty Payment */ Ok(None) },
            // EventType::PRD => { /* Purchase event, if used */ Ok(None) },
            _ => Ok(None),
        }
    }

    // =======================
    //        LAM Logic
    // =======================
    fn process_lam_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Payment
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let payment = state.accrued_interest;
                    state.accrued_interest = 0;
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            }

            // Principal Redemption
            EventType::PR => {
                // For LAM, typically a linear chunk each time or from terms.next_principal_redemption_payment
                let redemption_amt = terms.next_principal_redemption_payment.unwrap_or(0);
                let redemption = redemption_amt.min(state.notional_principal);
                state.notional_principal = state.notional_principal.saturating_sub(redemption);
                Ok(Some(redemption))
            }

            // Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let principal = state.notional_principal;
                        let interest = state.accrued_interest;
                        let total_payment = principal + interest;

                        // Zero out
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Additional event examples, if desired:
            // EventType::PD => { /* Principal Drawing for LAM, if used */ Ok(None) },
            // etc.
            _ => Ok(None),
        }
    }
    fn process_clm_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Payment
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let payment = state.accrued_interest;
                    state.accrued_interest = 0;
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            }

            // Call Option Exercise
            EventType::EXE => {
                // Check if exercise is allowed (could add more conditions)
                if let Some(exercise_date) = terms.exercise_date {
                    if timestamp >= exercise_date {
                        // Store exercise details in state
                        state.exercise_date = Some(timestamp);
                        state.exercise_amount = Some(state.notional_principal);

                        // Return early maturity payment
                        let total_payment = state.notional_principal + state.accrued_interest;
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Rate Reset - Important for CLM's variable rate nature
            EventType::RR => {
                if let Some(rate) = terms.next_reset_rate {
                    state.nominal_interest_rate = rate;

                    // Optional: Recalculate interest with new rate
                    update_accrued_interest(state, terms, timestamp)?;
                }
                Ok(None)
            }

            // Regular Interest Accrual
            EventType::AD => {
                update_accrued_interest(state, terms, timestamp)?;
                Ok(None)
            }

            // Termination
            EventType::TD => {
                // Handle early termination (if allowed in terms)
                if let Some(termination_date) = terms.termination_date {
                    if timestamp == termination_date {
                        let total_payment = state.notional_principal + state.accrued_interest;
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;
                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Principal Increase (Drawing)
            EventType::PD => {
                if let Some(drawing_amount) = terms.drawing_amount {
                    state.notional_principal = state
                        .notional_principal
                        .checked_add(drawing_amount)
                        .ok_or(Error::MathError("Principal increase overflow".into()))?;
                    Ok(Some(drawing_amount))
                } else {
                    Ok(None)
                }
            }

            // Principal Decrease (Repayment)
            EventType::PR => {
                if let Some(repayment_amount) = terms.repayment_amount {
                    let actual_repayment = repayment_amount.min(state.notional_principal);
                    state.notional_principal =
                        state.notional_principal.saturating_sub(actual_repayment);
                    Ok(Some(actual_repayment))
                } else {
                    Ok(None)
                }
            }

            _ => Ok(None),
        }
    }
    fn process_lax_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        // Apply scaling multiplier to relevant calculations
        let scaling_multiplier = state.notional_scaling_multiplier.max(1);

        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            // Apply initial scaling if present
                            let scaled_notional = notional
                                .checked_mul(scaling_multiplier)
                                .ok_or(Error::MathError("Scaling overflow".into()))?;

                            state.notional_principal = scaled_notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(scaled_notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Payment with scaling
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let scaled_interest = state
                        .accrued_interest
                        .checked_mul(state.interest_scaling_multiplier.max(1))
                        .ok_or(Error::MathError("Interest scaling overflow".into()))?;

                    state.accrued_interest = 0;
                    Ok(Some(scaled_interest))
                } else {
                    Ok(None)
                }
            }

            // Principal Redemption with scaling
            EventType::PR => {
                let base_redemption = terms.next_principal_redemption_payment.unwrap_or(0);
                let scaled_redemption = base_redemption
                    .checked_mul(scaling_multiplier)
                    .ok_or(Error::MathError("Redemption scaling overflow".into()))?;

                let actual_redemption = scaled_redemption.min(state.notional_principal);
                state.notional_principal =
                    state.notional_principal.saturating_sub(actual_redemption);

                Ok(Some(actual_redemption))
            }

            // Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        // Scale final payments
                        let scaled_principal = state
                            .notional_principal
                            .checked_mul(scaling_multiplier)
                            .ok_or(Error::MathError("Principal scaling overflow".into()))?;

                        let scaled_interest = state
                            .accrued_interest
                            .checked_mul(state.interest_scaling_multiplier.max(1))
                            .ok_or(Error::MathError("Interest scaling overflow".into()))?;

                        let total_payment = scaled_principal
                            .checked_add(scaled_interest)
                            .ok_or(Error::MathError("Payment addition overflow".into()))?;

                        // Zero out state
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;
                        state.notional_scaling_multiplier = 1;
                        state.interest_scaling_multiplier = 1;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Index Reset (new event type needed for LAX)
            EventType::IPCB => {
                // Update scaling multiplier based on index value
                if let Some(new_index) = terms.index_value {
                    state.notional_scaling_multiplier = new_index;
                }
                Ok(None)
            }

            // Regular interest accrual
            EventType::AD => {
                update_accrued_interest(state, terms, timestamp)?;
                Ok(None)
            }

            _ => Ok(None),
        }
    }

    // =======================
    //        NAM Logic
    // =======================
    fn process_nam_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // In NAM, interest might be capitalized rather than paid
            EventType::IP => {
                // If your NAM logic says skip IP, do that here
                Ok(None)
            }

            // Interest Capitalization
            EventType::IPCI => {
                let accrued = state.accrued_interest;
                state.notional_principal = state.notional_principal.saturating_add(accrued);
                state.accrued_interest = 0;
                Ok(Some(accrued)) // or Ok(None) if you don't consider a flow
            }

            // Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let total = state.notional_principal + state.accrued_interest;
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;
                        return Ok(Some(total));
                    }
                }
                Ok(None)
            }

            // Additional events, if desired
            // e.g., PP, PD, etc.
            _ => Ok(None),
        }
    }

    // =======================
    //        ANN Logic
    // =======================
    fn process_ann_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Annuity Payment (interest + principal)
            EventType::IP => {
                let outstanding = state.notional_principal;
                let interest_portion = state.accrued_interest;

                // Example stub for computing a fixed installment
                let annuity_payment = compute_annuity_payment(terms, outstanding)?;

                let principal_portion = annuity_payment.saturating_sub(interest_portion);

                // Update principal
                state.notional_principal = outstanding.saturating_sub(principal_portion);
                // Reset accrued interest
                state.accrued_interest = 0;

                Ok(Some(annuity_payment))
            }

            // Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let total_payment = state.notional_principal + state.accrued_interest;
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;
                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            _ => Ok(None),
        }
    }

    fn process_nax_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Capitalization (NAX specific)
            EventType::IPCI => {
                let accrued = state.accrued_interest;
                if accrued > 0 {
                    // Check for interest capitalization limit if specified
                    if let Some(cap_limit) = terms.capitalization_end_date {
                        if timestamp <= cap_limit {
                            state.notional_principal =
                                state.notional_principal.saturating_add(accrued);
                            state.accrued_interest = 0;
                            return Ok(Some(accrued));
                        }
                    }
                    // If past cap limit or no limit, handle as regular interest payment
                    state.accrued_interest = 0;
                    return Ok(Some(accrued));
                }
                Ok(None)
            }

            // Interest Payment (NAX allows partial payments)
            EventType::IP => {
                if state.accrued_interest > 0 {
                    // Check for minimum payment amount if specified
                    let payment_amount =
                        if let Some(min_payment) = terms.next_principal_redemption_payment {
                            min_payment.min(state.accrued_interest)
                        } else {
                            state.accrued_interest
                        };

                    state.accrued_interest -= payment_amount;
                    Ok(Some(payment_amount))
                } else {
                    Ok(None)
                }
            }

            // Principal Drawing (NAX specific)
            EventType::PD => {
                if let Some(draw_amount) = terms.next_principal_redemption_payment {
                    state.notional_principal = state.notional_principal.saturating_add(draw_amount);
                    Ok(Some(draw_amount))
                } else {
                    Ok(None)
                }
            }

            // Principal Redemption
            EventType::PR => {
                if let Some(redemption_amount) = terms.next_principal_redemption_payment {
                    let actual_redemption = redemption_amount.min(state.notional_principal);
                    state.notional_principal =
                        state.notional_principal.saturating_sub(actual_redemption);
                    Ok(Some(actual_redemption))
                } else {
                    Ok(None)
                }
            }

            // Rate Reset (NAX specific handling)
            EventType::RR => {
                if let Some(new_rate) = terms.nominal_interest_rate2 {
                    // Check for rate adjustment limits if specified
                    let current_rate = state.nominal_interest_rate;
                    state.nominal_interest_rate = new_rate;

                    // Recalculate accrued interest with new rate
                    update_accrued_interest(state, terms, timestamp)?;
                }
                Ok(None)
            }

            // Maturity
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        let total = state.notional_principal + state.accrued_interest;
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;
                        return Ok(Some(total));
                    }
                }
                Ok(None)
            }

            _ => Ok(None),
        }
    }

    pub fn process_anx_event(
        event: EventType,
        timestamp: Timestamp,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<u64>> {
        // Validate timestamp
        if timestamp < state.status_date {
            return Err(Error::ValidationError(String::from(
                "Event timestamp before status date",
            )));
        }

        // Update accrued interest if time has passed
        if timestamp > state.status_date {
            update_accrued_interest(state, &terms, timestamp);
        }

        // Process event
        let payment = match event {
            EventType::IED => process_initial_exchange(state, &terms, timestamp),
            EventType::IP => process_interest_payment(state, &terms, timestamp),
            EventType::PR => process_principal_redemption(state, &terms, timestamp),
            EventType::MD => process_maturity(state, &terms, timestamp),
            EventType::IPCI => process_interest_capitalization(state, &terms, timestamp),
            EventType::RR => process_rate_reset(state, &terms, timestamp),
            EventType::SC => process_scaling(state, &terms, timestamp),
            _ => Ok(None),
        };

        // Update status date
        state.status_date = timestamp;

        // Save updated state

        Ok(payment.unwrap())
    }

    fn process_pbn_event(
        event: EventType,
        timestamp: u64,
        state: &mut ContractState,
        terms: &ContractTerms,
    ) -> Result<Option<Units>> {
        match event {
            // Initial Exchange
            EventType::IED => {
                if let Some(ied) = terms.initial_exchange_date {
                    if timestamp == ied {
                        if let Some(notional) = terms.notional_principal {
                            state.notional_principal = notional;
                            state.nominal_interest_rate = terms.nominal_interest_rate.unwrap_or(0);
                            state.accrued_interest = 0;
                            return Ok(Some(notional));
                        }
                    }
                }
                Ok(None)
            }

            // Interest Payment
            EventType::IP => {
                if state.accrued_interest > 0 {
                    let payment = state.accrued_interest;
                    state.accrued_interest = 0;
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            }

            // Principal Redemption (non-balloon payments)
            EventType::PR => {
                // For PBN, make smaller regular payments, saving the balloon for maturity
                if let Some(redemption_amt) = terms.next_principal_redemption_payment {
                    // Calculate regular payment (excluding balloon)
                    let regular_payment = if let Some(balloon_amount) = terms.balloon_amount {
                        let remaining_principal =
                            state.notional_principal.saturating_sub(balloon_amount);
                        remaining_principal / terms.remaining_payments.unwrap_or(1)
                    } else {
                        redemption_amt
                    };

                    // Apply the regular payment
                    let payment = regular_payment.min(state.notional_principal);
                    state.notional_principal = state.notional_principal.saturating_sub(payment);
                    Ok(Some(payment))
                } else {
                    Ok(None)
                }
            }

            // Maturity (includes balloon payment)
            EventType::MD => {
                if let Some(md) = terms.maturity_date {
                    if timestamp == md {
                        // At maturity, pay all remaining principal (balloon) plus interest
                        let principal = state.notional_principal;
                        let interest = state.accrued_interest;
                        let total_payment = principal + interest;

                        // Zero out everything
                        state.notional_principal = 0;
                        state.accrued_interest = 0;
                        state.nominal_interest_rate = 0;

                        return Ok(Some(total_payment));
                    }
                }
                Ok(None)
            }

            // Regular Monitoring/Accrual
            EventType::AD => {
                update_accrued_interest(state, terms, timestamp)?;
                Ok(None)
            }

            // Rate Reset
            EventType::RR => {
                if let Some(rate) = terms.next_reset_rate {
                    state.nominal_interest_rate = rate;
                }
                Ok(None)
            }

            // Principal Prepayment
            EventType::PP => {
                if let Some(pp_effect) = terms.prepayment_effect {
                    match pp_effect {
                        PrepaymentEffect::PPEF_N => Ok(None),
                        PrepaymentEffect::PPEF_A => {
                            // Reduce next principal redemption
                            if state.next_principal_redemption_payment > 0 {
                                let amount = state.next_principal_redemption_payment;
                                state.next_principal_redemption_payment = 0;
                                Ok(Some(amount))
                            } else {
                                Ok(None)
                            }
                        }
                        PrepaymentEffect::PPEF_M => {
                            // Wipe out entire principal including balloon
                            let amount = state.notional_principal;
                            state.notional_principal = 0;
                            Ok(Some(amount))
                        }
                    }
                } else {
                    Ok(None)
                }
            }

            _ => Ok(None),
        }
    }
}
// =======================
//   Shared Logic
// =======================
/// Recalculates accrued interest from `status_date` to `timestamp` using day-count
pub fn update_accrued_interest(
    state: &mut ContractState,
    terms: &ContractTerms,
    timestamp: u64,
) -> Result<()> {
    // If the contract defines a day count convention, compute interest
    if let Some(dcc) = terms.day_count_convention {
        let value = dcc as u8;

        let time_fraction =
            math::year_fraction(value, state.status_date, timestamp, terms.maturity_date);

        // e.g. interest = principal * rate * time_fraction
        let accrual = state
            .notional_principal
            .checked_mul(state.nominal_interest_rate)
            .ok_or(Error::MathError("Interest calculation overflow".into()))?
            .checked_mul(time_fraction)
            .ok_or(Error::MathError("Interest calculation overflow".into()))?;

        // Add to accrued_interest
        state.accrued_interest = state
            .accrued_interest
            .checked_add(accrual)
            .ok_or(Error::MathError("Interest accrual overflow".into()))?;
    }

    Ok(())
}

/// Example placeholder for computing a fixed annuity payment
/// In reality, you'd use a more precise formula referencing period length, rate, etc.
fn compute_annuity_payment(terms: &ContractTerms, outstanding: Units) -> Result<Units> {
    // Could incorporate day-count fraction, rate, schedule, etc.
    // For demonstration, just return 1/10th of the outstanding
    let payment = outstanding / 10;
    Ok(payment)
}

// =======================
// Unit Tests
// =======================
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pam_initial_exchange() {
        let mut state = ContractState {
            status_date: 1000,
            notional_principal: 0,
            nominal_interest_rate: 0,
            accrued_interest: 0,
            contract_performance: ContractPerformance::PF,
            fee_accrued: 0,
            notional_scaling_multiplier: 0,
            interest_scaling_multiplier: 0,
            next_principal_redemption_payment: 0,
            interest_calculation_base: 0,
            exercise_date: None,
            exercise_amount: None,
            // Additional fields from your new struct:
            time_of_maturity: None,
            accrued_interest_first_leg: None,
            accrued_interest_second_leg: None,
            last_interest_period: None,
        };

        let terms = ContractTerms {
            contract_type: ContractType::PAM,
            initial_exchange_date: Some(1000),
            notional_principal: Some(1_000_000),
            nominal_interest_rate: Some(50_000), // 5% in basis points
            day_count_convention: None,
            schedule_config: ScheduleConfig {
                calendar: None,
                end_of_month_convention: None,
                business_day_convention: None,
            },
            status_date: 1000,
            // Fill in any other fields as needed
        };

        let result =
            TransitionEngine::process_event(EventType::IED, 1000, &mut state, &terms).unwrap();

        assert_eq!(result, Some(1_000_000));
        assert_eq!(state.notional_principal, 1_000_000);
        assert_eq!(state.nominal_interest_rate, 50_000);
        assert_eq!(state.accrued_interest, 0);
    }

    // Add more tests for LAM, NAM, ANN, etc.
}
