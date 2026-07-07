import { reactive } from "vue";
import type { FormRules } from "element-plus";
import { validatePasswordStrength } from "@/utils/password";

/** 登录校验 */
const loginRules = reactive<FormRules>({
  password: [
    {
      validator: (rule, value, callback) => {
        const err = validatePasswordStrength(value);
        if (err) {
          callback(new Error(err));
        } else {
          callback();
        }
      },
      trigger: "blur"
    }
  ]
});

export { loginRules };
