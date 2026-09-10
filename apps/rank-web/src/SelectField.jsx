import { useCallback, useId, useState } from "react";
import * as Select from "@radix-ui/react-select";
import { CaretDown, CaretUp, Check } from "@phosphor-icons/react";
import { menuConfig, menuStyle } from "./ui-config.js";

/**
 * Shared single selection with keyboard navigation and native form validation.
 * @param props Controlled string value, value callback, labeled options, and trigger attributes.
 * @returns A select whose menu stays inside its owning modal's browser top layer.
 */
export function SelectField({
  value,
  onValueChange,
  options,
  required = false,
  disabled = false,
  name,
  className = "",
  icon: Icon,
  ...props
}) {
  const emptyValue = useId();
  const [open, setOpen] = useState(false);
  const [container, setContainer] = useState(null);
  const attach = useCallback(
    (node) => setContainer(node?.closest("dialog") || null),
    [],
  );
  const emptyOption = options.find((option) => option.value === "");
  const selected =
    value === ""
      ? emptyOption && !emptyOption.disabled
        ? emptyValue
        : ""
      : value;

  return (
    <Select.Root
      value={selected}
      open={open}
      onOpenChange={setOpen}
      required={required}
      disabled={disabled}
      onValueChange={(next) => onValueChange(next === emptyValue ? "" : next)}
    >
      {name && (
        <input type="hidden" name={name} value={value} disabled={disabled} />
      )}
      <Select.Trigger
        ref={attach}
        className={`ui-select ${className}`}
        {...props}
      >
        {Icon && <Icon size={16} aria-hidden="true" />}
        <Select.Value placeholder={emptyOption?.label} />
        <Select.Icon asChild>
          <CaretDown size={14} />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal container={container}>
        <Select.Content
          className="ui-select-menu"
          style={menuStyle}
          position="popper"
          sideOffset={menuConfig.gap}
          collisionPadding={menuConfig.viewportPadding}
          onEscapeKeyDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
            setOpen(false);
          }}
        >
          <Select.ScrollUpButton className="ui-select-scroll">
            <CaretUp size={14} />
          </Select.ScrollUpButton>
          <Select.Viewport className="ui-select-options">
            {options.map((option) => (
              <Select.Item
                className="ui-select-option"
                key={option.value}
                value={option.value || emptyValue}
                disabled={option.disabled}
              >
                <Select.ItemText>{option.label}</Select.ItemText>
                <Select.ItemIndicator>
                  <Check size={15} weight="bold" />
                </Select.ItemIndicator>
              </Select.Item>
            ))}
          </Select.Viewport>
          <Select.ScrollDownButton className="ui-select-scroll">
            <CaretDown size={14} />
          </Select.ScrollDownButton>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  );
}
