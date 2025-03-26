use crate::Error;
use borsh::{BorshDeserialize, BorshSerialize};
use wasmlanche::{public, state_schema, Address, Context};
// Import common types and state schema

use super::math;
use super::types::*;

// ANX-specific constants
const BASIS_POINT_FACTOR: u64 = 10000; // 100% = 10000 basis points

pub fn process_initial_exchange(
    state: &mut ContractState,
    terms: &ContractTerms,
    timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
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

pub fn process_interest_payment(
    state: &mut ContractState,
    terms: &ContractTerms,
    timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    if state.accrued_interest == 0 {
        return Ok(None);
    }

    // Calculate annuity payment
    let annuity_payment = calculate_annuity_payment(state, terms)?;

    // Interest portion is the accrued interest
    let interest_portion = state.accrued_interest;

    // Principal portion is the remainder
    let principal_portion = if interest_portion >= annuity_payment {
        0
    } else {
        annuity_payment - interest_portion
    };

    // Update state
    state.accrued_interest = 0;
    state.notional_principal = state.notional_principal.saturating_sub(principal_portion);

    Ok(Some(annuity_payment))
}

pub fn process_principal_redemption(
    state: &mut ContractState,
    terms: &ContractTerms,
    _timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    if state.next_principal_redemption_payment == 0 {
        return Ok(None);
    }

    let payment = state.next_principal_redemption_payment;
    state.notional_principal = state.notional_principal.saturating_sub(payment);
    state.next_principal_redemption_payment = calculate_next_principal_payment(terms)?;

    Ok(Some(payment))
}

pub fn process_maturity(
    state: &mut ContractState,
    terms: &ContractTerms,
    timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    if let Some(md) = terms.maturity_date {
        if timestamp == md {
            let total = state.notional_principal + state.accrued_interest;

            // Clear state
            state.notional_principal = 0;
            state.accrued_interest = 0;
            state.nominal_interest_rate = 0;

            return Ok(Some(total));
        }
    }
    Ok(None)
}

pub fn process_interest_capitalization(
    state: &mut ContractState,
    _terms: &ContractTerms,
    _timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    let accrued = state.accrued_interest;
    state.notional_principal = state.notional_principal.saturating_add(accrued);
    state.accrued_interest = 0;
    Ok(Some(accrued))
}

pub fn process_rate_reset(
    state: &mut ContractState,
    terms: &ContractTerms,
    _timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    if let Some(new_rate) = terms.nominal_interest_rate {
        state.nominal_interest_rate = new_rate;
    }
    Ok(None)
}

pub fn process_scaling(
    state: &mut ContractState,
    terms: &ContractTerms,
    _timestamp: Timestamp,
) -> Result<Option<Units>, ()> {
    if let Some(scaling_effect) = terms.scaling_effect {
        if let Some(index) = terms.scaling_index_at_status_date {
            match scaling_effect {
                ScalingEffect::SE_INO | ScalingEffect::SE_INM => {
                    state.notional_scaling_multiplier = calculate_scaling_multiplier(index)?;
                }
                ScalingEffect::SE_IOM | ScalingEffect::SE_INM => {
                    state.interest_scaling_multiplier = calculate_scaling_multiplier(index)?;
                }
                _ => {}
            }
        }
    }
    Ok(None)
}



pub fn calculate_annuity_payment(
    state: &ContractState,
    terms: &ContractTerms,
) -> Result<Units, ()> {
    // This is a simplified calculation - in practice, you'd use a more sophisticated
    // formula taking into account remaining periods, interest rate, etc.
    let periods_remaining = calculate_remaining_periods(terms)?;
    if periods_remaining == 0 {
        return Ok(0);
    }

    let total_due = state.notional_principal + state.accrued_interest;
    Ok(total_due / periods_remaining)
}

pub fn calculate_remaining_periods(terms: &ContractTerms) -> Result<Units, ()> {
    // Simplified - you'd typically calculate this based on the payment schedule
    if let (Some(maturity), Some(cycle)) = (
        terms.maturity_date,
        terms.cycle_of_principal_redemption.clone(),
    ) {
        Ok((maturity / (cycle.n as u64)).max(1))
    } else {
        Ok(1) // Default to 1 if we can't calculate
    }
}

pub fn calculate_next_principal_payment(terms: &ContractTerms) -> Result<Units, ()> {
    if let Some(notional) = terms.notional_principal {
        let periods = calculate_remaining_periods(terms)?;
        Ok(notional / periods)
    } else {
        Ok(0)
    }
}

pub fn calculate_scaling_multiplier(index: Units) -> Result<Units, ()> {
    if index == 0 {
        return Ok(BASIS_POINT_FACTOR);
    }
    Ok((index * BASIS_POINT_FACTOR) / BASIS_POINT_FACTOR)
}

#[cfg(test)]
mod tests {
    use super::*;

    // Add tests for the ANX contract implementation
    #[test]
    fn test_initial_exchange() {
        // Test initialization and IED event
    }

    #[test]
    fn test_annuity_payment() {
        // Test regular annuity payment calculation and processing
    }

    #[test]
    fn test_interest_capitalization() {
        // Test IPCI event
    }
}
